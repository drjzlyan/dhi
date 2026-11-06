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
// tree (F-064). It is read through go-git, whose status hashes the
// worktree (seconds on a big repo), so it is read off the UI goroutine:
// rows render from this cache, and a save or git action marks it stale
// and reloads it in the background while the old letters stay up.
type treeGit struct {
	branch  string
	changed map[string]byte // repo-relative slash path → status letter
	dirs    map[string]bool // repo-relative dirs holding a change

	root    string
	loading bool // a read is in flight
	stale   bool // invalidated since the last read: reload on next use
}

// treeGitResult is a finished background read for one member.
type treeGitResult struct {
	member string
	gen    int
	g      *treeGit
}

// gitFor returns the cached status for member and starts a read when
// there is none yet or it went stale. Called from render: never blocks.
func (m *Model) gitFor(member, root string) *treeGit {
	g, ok := m.treeGits[member]
	if !ok {
		g = &treeGit{changed: map[string]byte{}, dirs: map[string]bool{}, root: root, stale: true}
		if m.treeGits == nil {
			m.treeGits = map[string]*treeGit{}
		}
		m.treeGits[member] = g
	}
	if g.stale && !g.loading {
		m.startTreeGit(member, g)
	}
	return g
}

// startTreeGit reads member's status off the UI goroutine;
// treeGitMsgDone carries it back to applyTreeGit.
func (m *Model) startTreeGit(member string, g *treeGit) {
	g.stale = false
	if g.root == "" || m.termMsgs == nil {
		return
	}
	g.loading = true
	ch, root, gen := m.termMsgs, g.root, m.treeGitGen
	go func() {
		ch <- teaMsg{kind: treeGitMsgDone, treeGit: &treeGitResult{member: member, gen: gen, g: readTreeGit(root)}}
	}()
}

// readTreeGit reads root's branch and changed paths. Non-repositories
// give an empty entry so they are not retried.
func readTreeGit(root string) *treeGit {
	g := &treeGit{changed: map[string]byte{}, dirs: map[string]bool{}, root: root}
	if !gitcore.IsRepo(root) {
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

// applyTreeGit stores a finished read unless the cache was invalidated
// after it started (a newer read is then on its way).
func (m *Model) applyTreeGit(r *treeGitResult) {
	if r == nil || r.gen != m.treeGitGen || m.treeGits == nil {
		return
	}
	m.treeGits[r.member] = r.g
	m.refreshRows() // the rows carry the letters
}

// invalidateTreeGit marks the cached tree status stale: rows keep the old
// letters until the background reload lands.
func (m *Model) invalidateTreeGit() {
	m.treeGitGen++
	for _, g := range m.treeGits {
		g.stale, g.loading = true, false
	}
}

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
// amber, deleted red, anything else (renames, conflicts) indigo.
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
