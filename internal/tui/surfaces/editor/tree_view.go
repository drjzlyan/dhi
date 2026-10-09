package editor

import (
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// treeGit is one member's branch and changed paths, shown in the file
// tree (F-064). It is read once per member through go-git (no shelling
// out) and dropped on save or after a git action, so rows render from a
// cache instead of hitting the repository on every keypress.
type treeGit struct {
	branch  string
	changed map[string]byte // repo-relative slash path → status letter
	dirs    map[string]bool // repo-relative dirs holding a change
}

// gitFor returns the cached status for member, loading it on first use.
// Non-repositories cache an empty entry so they are not retried.
func (m *Model) gitFor(member, root string) *treeGit {
	if g, ok := m.treeGits[member]; ok {
		return g
	}
	g := &treeGit{changed: map[string]byte{}, dirs: map[string]bool{}}
	if m.treeGits == nil {
		m.treeGits = map[string]*treeGit{}
	}
	m.treeGits[member] = g
	if root == "" || !gitcore.IsRepo(root) {
		return g
	}
	rp, err := gitcore.Open(root)
	if err != nil {
		return g
	}
	g.branch, _ = rp.CurrentBranch()
	st, err := rp.Status()
	if err != nil {
		return g
	}
	for _, f := range st {
		letter := f.Y
		if letter == ' ' || letter == 0 {
			letter = f.X
		}
		p := filepath.ToSlash(f.Path)
		g.changed[p] = letter
		for d := filepath.ToSlash(filepath.Dir(p)); d != "." && d != "/"; d = filepath.ToSlash(filepath.Dir(d)) {
			g.dirs[d] = true
		}
	}
	return g
}

// invalidateTreeGit drops the cached tree status so the next render
// reloads it.
func (m *Model) invalidateTreeGit() { m.treeGits = nil }

// memberRoot is the filesystem root of member.
func (m *Model) memberRoot(member string) string {
	for _, mem := range m.members {
		if mem.name == member {
			return mem.path
		}
	}
	return ""
}

// gitLetter styles a status letter: added/untracked green, modified
// amber, deleted red, anything else (renames, conflicts) violet.
func gitLetter(b byte) string {
	var c = theme.Current.Accent2
	switch b {
	case 'A', '?':
		c = theme.Current.Success
	case 'M':
		c = theme.Current.Warning
	case 'D':
		c = theme.Current.Danger
	}
	if b == '?' {
		b = 'U' // untracked reads as "U", like most editors
	}
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(b))
}

// treeItem renders one tree row (F-064): repo roots as a header with the
// branch; folders and files under guide rails ("├ ", "└ ", "│ "),
// folders with an open/closed chevron, files with a colored kind glyph,
// and a git status letter on the right.
func (m *Model) treeItem(r treeRow) kit.Item {
	n := r.node
	g := m.gitFor(n.member, m.memberRoot(n.member))
	rel := ""
	if root := m.memberRoot(n.member); root != "" {
		if p, err := filepath.Rel(root, n.path); err == nil {
			rel = filepath.ToSlash(p)
		}
	}

	if n.kind == nodeRepo {
		chev := theme.GlyphDirClosed
		if n.expanded {
			chev = theme.GlyphDirOpen
		}
		title := theme.TextMuted().Render(chev+" ") + theme.AccentText().Render(n.name+"/")
		if g.branch != "" {
			title += " " + kit.Pill(g.branch, theme.Current.TextDim)
		}
		it := kit.Item{Title: title}
		if len(g.changed) > 0 {
			it.Tag = theme.WarningText().Render(theme.GlyphDot)
		}
		return it
	}

	conn := "├ "
	if r.last {
		conn = "└ "
	}
	rail := theme.RuleText().Render(r.guide + conn)
	if n.kind == nodeDir {
		chev := theme.GlyphDirClosed
		if n.expanded {
			chev = theme.GlyphDirOpen
		}
		it := kit.Item{Title: rail + theme.TextMuted().Render(chev+" ") + theme.InfoText().Render(n.name+"/")}
		if g.dirs[rel] {
			it.Tag = theme.WarningText().Render(theme.GlyphDot)
		}
		return it
	}

	glyph, c := theme.FileKind(n.name)
	name := n.name
	nameSt := theme.TextStyle()
	if strings.HasPrefix(name, ".") {
		nameSt = theme.TextMuted()
	}
	it := kit.Item{Title: rail + lipgloss.NewStyle().Foreground(c).Render(glyph) + " " + nameSt.Render(name)}
	if b, ok := g.changed[rel]; ok {
		it.Tag = gitLetter(b)
	}
	return it
}
