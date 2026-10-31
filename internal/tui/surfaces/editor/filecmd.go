package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// fileCommand handles :e <path> and :w <path> / :w! <path> (F-119).
func (m *Model) fileCommand(requester *textbuf.Editor, cmd string) (string, bool) {
	if strings.TrimSpace(cmd) == "reopen" {
		return m.reopenClosed(), true
	}
	verb, arg, ok := strings.Cut(strings.TrimSpace(cmd), " ")
	arg = strings.TrimSpace(arg)
	if !ok || arg == "" {
		return "", false
	}
	switch verb {
	case "e", "edit":
		if arg == "#" { // the alternate file: the one closed last
			return m.reopenClosed(), true
		}
		return m.editPath(requester, arg), true
	case "w", "w!", "saveas":
		return m.saveAs(requester, arg, verb == "w!"), true
	}
	return "", false
}

// resolvePath reads p as absolute, a workspace path ("member/dir/file"),
// or relative to the current file's folder, then the first member.
func (m *Model) resolvePath(requester *textbuf.Editor, p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	var tries []string
	if requester != nil && requester.Path() != "" {
		tries = append(tries, filepath.Join(filepath.Dir(requester.Path()), p))
	}
	if m.ws != nil {
		if vp, err := workspace.ParseVPath(p); err == nil {
			if abs, err := m.ws.Resolve(vp); err == nil {
				tries = append(tries, abs)
			}
		}
	}
	if len(m.members) > 0 {
		tries = append(tries, filepath.Join(m.members[0].path, p))
	}
	for _, t := range tries {
		if _, err := os.Stat(t); err == nil {
			return t
		}
	}
	if len(tries) > 0 {
		return tries[0]
	}
	return p
}

// editPath opens path in a tab; a path that does not exist yet becomes a
// new, empty file (inside the workspace only).
func (m *Model) editPath(requester *textbuf.Editor, p string) string {
	target := m.resolvePath(requester, p)
	if fi, err := os.Stat(target); err == nil && fi.IsDir() {
		m.revealPath(target)
		m.bufFocus = false
		return "opened the folder in the tree"
	} else if err != nil {
		if m.memberRootOf(target) == "" {
			return "no such file: " + p
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err.Error()
		}
		if err := os.WriteFile(target, nil, 0o644); err != nil {
			return err.Error()
		}
		m.refreshTree()
	}
	m.open(&node{kind: nodeFile, path: target, name: filepath.Base(target)})
	return ""
}

// saveAs writes the buffer to a new path and keeps editing it there (as
// "Save As" does); an existing file needs :w!.
func (m *Model) saveAs(e *textbuf.Editor, p string, force bool) string {
	if e == nil || m.activeTabRef() == nil || m.activeTabRef().ed != e {
		return "open a file first"
	}
	target := m.resolvePath(e, p)
	if _, err := os.Stat(target); err == nil && !force && target != e.Path() {
		return filepath.Base(target) + " exists — :w! " + p + " overwrites it"
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err.Error()
	}
	if err := e.Buffer().SaveForce(target); err != nil {
		return err.Error()
	}
	t := m.activeTabRef()
	t.path = target
	e.SetPath(target)
	if vp, err := m.ws.VPathFor(target); err == nil {
		t.vp = vp.String()
	} else {
		t.vp = target
	}
	t.syntax = &highlighter{path: target}
	m.openPath, m.openVPath = t.path, t.vp
	m.items = nil
	m.refreshTree()
	m.lspOpenDoc(target, e.Buffer().Text())
	return fmt.Sprintf("%q written — now editing it", t.vp)
}

// indentFor is what Tab inserts in path (F-119): the file's own style when
// it has one, else the language's (tabs for Go, Makefiles), else four
// spaces.
func (m *Model) indentFor(path, text string) string {
	if unit := textbuf.DetectIndent(text); unit != "" {
		return unit
	}
	base := strings.ToLower(filepath.Base(path))
	if base == "makefile" || strings.HasSuffix(base, ".mk") {
		return "\t"
	}
	if l, _, ok := m.langs.For(path); ok {
		if l.UseTabs {
			return "\t"
		}
		if l.Width > 0 {
			return strings.Repeat(" ", l.Width)
		}
	}
	return "    "
}
