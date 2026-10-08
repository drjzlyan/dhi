package editor

import (
	"os"
	"strings"

	"github.com/drjzlyan/dhi/internal/projreplace"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// replaceState is the project-replace prompt over the search results
// (F-056): the replacement being typed and the live preview it gives.
type replaceState struct {
	input []rune
	plan  []projreplace.Change
	err   string
}

// openReplace starts the replace prompt for the current results.
func (m *Model) openReplace() {
	if len(m.hits) == 0 || m.searching {
		return
	}
	m.replace = &replaceState{}
	m.replanReplace()
}

// replanReplace recomputes the preview for the typed replacement.
func (m *Model) replanReplace() {
	r := m.replace
	hits := make([]search.Hit, len(m.hits))
	for i, h := range m.hits {
		hits[i] = h.hit
	}
	plan, err := projreplace.Plan(hits, m.lastQueryText, m.lastQueryRegex, string(r.input))
	r.plan, r.err = plan, ""
	if err != nil {
		r.err = err.Error()
	}
}

func (m *Model) handleReplaceKey(key string) bool {
	r := m.replace
	switch key {
	case "esc":
		m.replace = nil // nothing written
		return true
	case "enter":
		if r.err != "" || len(r.plan) == 0 {
			return true
		}
		res := projreplace.Apply(r.plan, editorFiles{m})
		m.replace = nil
		m.searchErr = ""
		m.startSearch(m.lastQueryText) // what remains, if anything
		m.replaceNote = res.Summary()
		return true
	case "backspace":
		if len(r.input) > 0 {
			r.input = r.input[:len(r.input)-1]
		}
	case "space":
		r.input = append(r.input, ' ')
	default:
		rs := []rune(key)
		if len(rs) != 1 || rs[0] < 32 {
			return true // the prompt owns the keyboard
		}
		r.input = append(r.input, rs[0])
	}
	m.replanReplace()
	return true
}

// replaceBlock renders the prompt and the preview of every change.
func (m *Model) replaceBlock(w int) string {
	r := m.replace
	mode := "fixed string"
	if m.lastQueryRegex {
		mode = "regex · $1 uses a group"
	}
	lines := []string{
		theme.TabActive().Render("replace "+quote(m.lastQueryText)) + theme.Hint().Render("  ("+mode+")"),
		theme.Brand().Render("with › "+string(r.input)) + "▏",
		"",
	}
	switch {
	case r.err != "":
		lines = append(lines, theme.DangerText().Render(r.err))
	case len(r.plan) == 0:
		lines = append(lines, theme.TextDim().Render("no line changes with this replacement"))
	default:
		files := map[string]bool{}
		open := 0
		for _, c := range r.plan {
			if !files[c.Path] {
				files[c.Path] = true
				if m.bufferFor(c.Path) != nil {
					open++
				}
			}
		}
		lines = append(lines, theme.Hint().Render(itoa(len(r.plan))+" line(s) in "+itoa(len(files))+" file(s) — "+
			itoa(open)+" open buffer(s) change in place (undo with u), "+itoa(len(files)-open)+" written to disk"), "")
		for _, c := range r.plan {
			label := c.Path
			if vp, err := m.ws.VPathFor(c.Path); err == nil {
				label = vp.String()
			}
			lines = append(lines,
				theme.Hint().Render(kit.ClipEllipsis(label+":"+itoa(c.Line), w)),
				"  "+theme.DangerText().Render("- "+kit.ClipEllipsis(strings.TrimSpace(c.Old), w-4)),
				"  "+theme.SuccessText().Render("+ "+kit.ClipEllipsis(strings.TrimSpace(c.New), w-4)))
		}
	}
	lines = append(lines, "", theme.Hint().Render("enter replace all · esc cancel"))
	return strings.Join(lines, "\n")
}

func quote(s string) string { return "\"" + s + "\"" }

// bufferFor returns the open buffer editing abs, if any.
func (m *Model) bufferFor(abs string) *bufTab {
	for _, t := range m.bufs {
		if t.path == abs {
			return t
		}
	}
	return nil
}

// editorFiles is projreplace's Files seam over the editor: open buffers
// are edited in place (one undo step, unsaved like any edit), other files
// on disk.
type editorFiles struct{ m *Model }

func (f editorFiles) Lines(path string) ([]string, error) {
	if t := f.m.bufferFor(path); t != nil {
		return t.ed.Buffer().Lines(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), nil
}

func (f editorFiles) SetLines(path string, edits map[int]string) error {
	if t := f.m.bufferFor(path); t != nil {
		t.ed.Buffer().ReplaceLines(edits)
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	trailing := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, l := range edits {
		if i >= 0 && i < len(lines) {
			lines[i] = l
		}
	}
	out := strings.Join(lines, "\n")
	if trailing {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), info.Mode().Perm())
}
