package editor

import (
	"fmt"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Context mentions in the chat (F-120): @file, @selection, @problems and
// @<path> put the editor's state into the message so the agent starts
// from what you are looking at. Agent names stay mentions.

var mentionRe = regexp.MustCompile(`@([\w./~-]+)`)

// contextWords are the mentions that are not agents.
var contextWords = []string{"file", "selection", "problems"}

// expandMentions rewrites a chat message: each context mention becomes a
// reference in the text, and the content it names is attached below.
// chips names what was attached, for the composer.
func (m *Model) expandMentions(text string, agents []string) (out string, chips []string) {
	isAgent := map[string]bool{}
	for _, a := range agents {
		isAgent[a] = true
	}
	var attach []string
	seen := map[string]bool{}
	out = mentionRe.ReplaceAllStringFunc(text, func(tok string) string {
		name := strings.TrimSuffix(tok[1:], ".")
		if isAgent[name] {
			return tok
		}
		ref, block, chip, ok := m.mentionContext(name)
		if !ok {
			return tok
		}
		if !seen[name] {
			seen[name] = true
			chips = append(chips, chip)
			if block != "" {
				attach = append(attach, block)
			}
		}
		return ref
	})
	if len(attach) > 0 {
		out += "\n\n" + strings.Join(attach, "\n\n")
	}
	return out, chips
}

// agentPath is how an agent (running in the workspace root) finds path.
func (m *Model) agentPath(abs string) string {
	if m.ws != nil {
		if r, err := filepath.Rel(m.ws.Root, abs); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return abs
}

// mentionContext resolves one mention: the text that replaces it, a block
// to attach, and a chip label.
func (m *Model) mentionContext(name string) (ref, block, chip string, ok bool) {
	e := m.active()
	switch name {
	case "file":
		if e == nil || e.Path() == "" {
			return "", "", "", false
		}
		p := m.agentPath(e.Path())
		return "`" + p + "`", "", filepath.Base(p), true
	case "selection":
		if e == nil {
			return "", "", "", false
		}
		sel, from, to, has := e.Selection()
		if !has {
			l := e.Buffer().Cursor().Line
			sel, from, to = e.Buffer().Line(l), e.Buffer().Cursor(), e.Buffer().Cursor()
			from.Col, to.Col = 0, len([]rune(sel))
		}
		p := m.agentPath(e.Path())
		n := strings.Count(sel, "\n") + 1
		return "the selection below", fmt.Sprintf("%s lines %d-%d:\n```%s\n%s\n```",
				p, from.Line+1, to.Line+1, fenceLang(p), sel),
			fmt.Sprintf("selection (%d %s)", n, map[bool]string{true: "line", false: "lines"}[n == 1]), true
	case "problems", "diagnostics":
		if e == nil {
			return "", "", "", false
		}
		ds := m.sortedDiags(e.Path())
		if len(ds) == 0 {
			return "the problems (none reported)", "", "no problems", true
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Problems in %s:\n", m.agentPath(e.Path()))
		for _, d := range ds {
			fmt.Fprintf(&b, "- line %d: %s\n", d.Line+1, strings.Join(strings.Fields(d.Message), " "))
		}
		return "the problems below", strings.TrimRight(b.String(), "\n"), kit.Count(len(ds), "problem"), true
	}
	if !strings.ContainsAny(name, "./") { // a plain word: an unknown agent, leave it
		return "", "", "", false
	}
	abs := m.resolvePath(e, name)
	if _, err := os.Stat(abs); err != nil {
		return "", "", "", false
	}
	p := m.agentPath(abs)
	return "`" + p + "`", "", filepath.Base(strings.TrimSuffix(p, "/")), true
}

func fenceLang(p string) string {
	return strings.TrimPrefix(filepath.Ext(p), ".")
}

// mentionSuggestions are the completions for a partly typed "@word".
func (m *Model) mentionSuggestions(prefix string, agents []string) []string {
	var out []string
	add := func(s string) {
		if strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix)) && len(out) < 6 {
			for _, o := range out {
				if o == s {
					return
				}
			}
			out = append(out, s)
		}
	}
	for _, a := range agents {
		add(a)
	}
	for _, w := range contextWords {
		add(w)
	}
	var files []string
	for _, t := range m.bufs {
		files = append(files, m.agentPath(t.path))
	}
	for _, p := range m.recentShown(10) {
		files = append(files, m.agentPath(p))
	}
	sort.SliceStable(files, func(i, j int) bool { return len(files[i]) < len(files[j]) })
	for _, f := range files {
		add(f)
		if strings.HasPrefix(strings.ToLower(filepath.Base(f)), strings.ToLower(prefix)) {
			if len(out) < 6 && !contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// chipLine renders what a message will carry.
func chipLine(chips []string) string {
	if len(chips) == 0 {
		return ""
	}
	return theme.AccentText().Render("+ ") + theme.TextDim().Render(strings.Join(chips, " · "))
}
