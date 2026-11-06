package editor

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Branch graph (F-162): :graph, or g in the git panel, shows `git log --graph
// --all` for the member repo. enter opens the commit's whole diff read-only,
// c cherry-picks it onto the current branch after a y/n, b makes a branch at
// it. Runs the user's own git (ADR-0044).

const graphLimit = 500

type graphView struct {
	dir     string
	rows    []gitcore.GraphRow
	sel     int       // index into rows; always a commit row
	confirm bool      // c pressed: y picks, anything else cancels
	naming  bool      // typing the name of a branch at sel
	input   kit.Input // the branch name
	note    string    // a neutral result (conflict guidance, success)
	err     string
}

func (m *Model) openGraph() string {
	if m.gitCLI == nil {
		return "git is not available"
	}
	dir := m.graphRoot()
	if dir == "" {
		return "no git repository here"
	}
	g := &graphView{dir: dir, input: kit.NewInput("", "new branch name")}
	if msg := m.loadGraph(g); msg != "" {
		return msg
	}
	m.graph = g
	return ""
}

// graphRoot is the repo the git panel shows, else the one under the open file.
func (m *Model) graphRoot() string {
	if m.gitRepo != nil {
		return m.gitRepo.Path()
	}
	if root := m.currentGitRoot(); root != "" && gitcore.IsRepo(root) {
		return root
	}
	return ""
}

func (m *Model) loadGraph(g *graphView) string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows, err := gitcore.GraphLog(ctx, m.gitCLI, g.dir, graphLimit)
	if err != nil {
		return "graph: " + err.Error()
	}
	if len(rows) == 0 {
		return "no commits yet"
	}
	g.rows = rows
	g.sel = g.commitFrom(min(g.sel, len(rows)-1), 1)
	return ""
}

// commitFrom is the nearest commit row at or after i going dir (+1/-1),
// else the nearest the other way.
func (g *graphView) commitFrom(i, dir int) int {
	for j := i; j >= 0 && j < len(g.rows); j += dir {
		if g.rows[j].IsCommit() {
			return j
		}
	}
	for j := i; j >= 0 && j < len(g.rows); j -= dir {
		if g.rows[j].IsCommit() {
			return j
		}
	}
	return 0
}

// step moves the selection n commits (negative: up), stopping at the ends.
func (g *graphView) step(n int) {
	dir := 1
	if n < 0 {
		dir, n = -1, -n
	}
	for ; n > 0; n-- {
		j := g.sel + dir
		for j >= 0 && j < len(g.rows) && !g.rows[j].IsCommit() {
			j += dir
		}
		if j < 0 || j >= len(g.rows) {
			return
		}
		g.sel = j
	}
}

// splitCommitPatch cuts a whole commit's patch into one entry per hunk; each
// starts with its file's name so a hunk stands alone. A file with no hunks
// (binary, mode change, pure rename) becomes one entry holding its header.
func splitCommitPatch(patch string) []string {
	var hunks []string
	for _, sec := range strings.Split("\n"+patch, "\ndiff --git ")[1:] {
		head, body, _ := strings.Cut(sec, "\n")
		name := head
		if i := strings.LastIndex(head, " b/"); i >= 0 {
			name = head[i+3:]
		}
		_, hs := splitPatch(body + "\n")
		if len(hs) == 0 {
			hunks = append(hunks, name+"  (no text changes)\n"+strings.TrimRight(body, "\n")+"\n")
			continue
		}
		for _, h := range hs {
			hunks = append(hunks, name+"  "+h)
		}
	}
	return hunks
}

func (m *Model) graphKey(key string) bool {
	g := m.graph
	if g.naming {
		switch key {
		case "esc":
			g.naming = false
		case "enter":
			m.graphBranch()
		default:
			g.input.HandleKey(key)
		}
		return true
	}
	if g.confirm {
		g.confirm = false
		if key == "y" {
			m.graphPick()
		}
		return true
	}
	g.err = ""
	switch key {
	case "esc", "q":
		m.graph = nil
	case "j", "down":
		g.step(1)
	case "k", "up":
		g.step(-1)
	case "pgdown", "ctrl+d":
		g.step(graphRows)
	case "pgup", "ctrl+u":
		g.step(-graphRows)
	case "enter":
		c := g.rows[g.sel]
		d := &diffView{path: c.Short + " " + c.Subject, rev: c.Hash, dir: g.dir, whole: true}
		m.loadDiff(d)
		m.diff = d
	case "c":
		c := g.rows[g.sel]
		switch {
		case len(c.Parents) > 1:
			g.err = "a merge commit has no single change to pick — pick one of its parents' commits"
		case len(c.Parents) == 0:
			g.err = "the root commit cannot be cherry-picked"
		default:
			g.note, g.confirm = "", true
		}
	case "b":
		g.note, g.naming = "", true
		g.input = kit.NewInput("", "new branch name")
	}
	return true
}

func (m *Model) graphCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// graphPick cherry-picks the selected commit; a conflict is guidance, not a failure.
func (m *Model) graphPick() {
	g := m.graph
	c := g.rows[g.sel]
	ctx, cancel := m.graphCtx()
	defer cancel()
	err := gitcore.CherryPick(ctx, m.gitCLI, g.dir, c.Hash)
	if err == nil {
		g.err, g.note = "", "cherry-picked "+c.Short
		m.stashDone(g.note)
		m.loadGraph(g)
		return
	}
	if gitcore.InCherryPick(ctx, m.gitCLI, g.dir) {
		g.err, g.note = "", m.pickStopped(ctx, g.dir)
		m.stashDone("")
		return
	}
	g.err = err.Error()
}

// pickStopped describes a cherry-pick git stopped halfway.
func (m *Model) pickStopped(ctx context.Context, dir string) string {
	cs, _ := gitcore.Conflicts(ctx, m.gitCLI, dir)
	if len(cs) == 0 {
		return "the pick leaves nothing new to commit — :cherrypick abort drops it"
	}
	var names []string
	for _, c := range cs {
		names = append(names, c.Path)
	}
	return "conflict in " + strings.Join(names, ", ") + " — esc, then ]x / :conflict to resolve and stage, " +
		"git commit to finish; or :cherrypick abort"
}

// graphBranch makes the typed branch at the selected commit.
func (m *Model) graphBranch() {
	g := m.graph
	c := g.rows[g.sel]
	name := strings.TrimSpace(g.input.Value)
	ctx, cancel := m.graphCtx()
	defer cancel()
	if err := gitcore.BranchAt(ctx, m.gitCLI, g.dir, name, c.Hash); err != nil {
		g.err = err.Error()
		return
	}
	g.naming, g.err, g.note = false, "", "branch "+name+" at "+c.Short
	m.gitMessage = g.note
	m.loadGraph(g)
}

// cherryPickCmd is :cherrypick (usage) and :cherrypick abort.
func (m *Model) cherryPickCmd(abort bool) string {
	if !abort {
		return "usage: :cherrypick abort — cherry-pick itself starts from :graph (c)"
	}
	dir := m.graphRoot()
	if m.gitCLI == nil || dir == "" {
		return "no git repository here"
	}
	ctx, cancel := m.graphCtx()
	defer cancel()
	if !gitcore.InCherryPick(ctx, m.gitCLI, dir) {
		return "no cherry-pick in progress"
	}
	if err := gitcore.CherryPickAbort(ctx, m.gitCLI, dir); err != nil {
		return "cherry-pick abort: " + err.Error()
	}
	m.stashDone("cherry-pick aborted")
	if m.graph != nil {
		m.graph.note, m.graph.err = "cherry-pick aborted", ""
	}
	return "cherry-pick aborted"
}

const graphRows = 14

// graphArt colours the lane art: each column pair is one lane.
func graphArt(art string) string {
	var b strings.Builder
	col := 0
	for _, r := range art {
		s := string(r)
		if r == ' ' {
			b.WriteString(s)
		} else {
			b.WriteString(theme.GraphLaneText(col / 2).Render(s))
		}
		col++
	}
	return b.String()
}

func refPill(r gitcore.GraphRef) string {
	switch r.Kind {
	case gitcore.RefHead:
		return theme.AccentText().Render(r.Name)
	case gitcore.RefBranch:
		if r.Current {
			return theme.SuccessText().Bold(true).Render(r.Name)
		}
		return theme.SuccessText().Render(r.Name)
	case gitcore.RefRemote:
		return theme.InfoText().Render(r.Name)
	case gitcore.RefTag:
		return theme.WarningText().Render("tag:" + r.Name)
	}
	return theme.TextDim().Render(r.Name)
}

// graphLine renders one row within w cells.
func graphLine(r gitcore.GraphRow, sel bool, w int) string {
	art := graphArt(r.Graph)
	if !r.IsCommit() {
		return "  " + art
	}
	used := 2 + utf8.RuneCountInString(r.Graph) + 1
	var refs []string
	plain := 0
	for _, ref := range r.Refs {
		refs = append(refs, refPill(ref))
		plain += utf8.RuneCountInString(ref.Name) + 1
		if ref.Kind == gitcore.RefTag {
			plain += 4
		}
	}
	subject := kit.ClipEllipsis(r.Subject, max(w-used-plain-2, 8))
	if sel {
		subject = theme.TabActive().Render(subject)
	} else {
		subject = theme.TextStyle().Render(subject)
	}
	line := art + " " + strings.Join(append(refs, subject), " ")
	if tail := r.Author + " · " + r.When; used+plain+utf8.RuneCountInString(r.Subject)+2+utf8.RuneCountInString(tail) <= w {
		line += "  " + theme.TextDim().Render(tail)
	}
	if sel {
		return theme.GlyphCursor + " " + line
	}
	return "  " + line
}

func (m *Model) graphOverlay(base string) string {
	g := m.graph
	w := min(m.width-4, 110)
	var lines []string
	start := max(0, min(g.sel-graphRows/2, len(g.rows)-graphRows))
	for i := start; i < len(g.rows) && i < start+graphRows; i++ {
		lines = append(lines, graphLine(g.rows[i], i == g.sel, w-4))
	}
	c := g.rows[g.sel]
	keys := "enter diff · c cherry-pick · b branch here · ↑/↓ pgup/pgdn move · esc close"
	switch {
	case g.naming:
		lines = append(lines, "", theme.TextMuted().Render("new branch at "+c.Short))
		lines = append(lines, g.input.View(48)...)
		keys = "enter create · esc back"
	case g.confirm:
		keys = "cherry-pick " + c.Short + " onto the current branch? y picks · any other key cancels"
	}
	if g.note != "" && !g.confirm && !g.naming {
		lines = append(lines, "", theme.WarningText().Render(g.note))
	}
	lines = append(lines, "", theme.Hint().Render(keys))
	md := kit.Modal{Title: "graph · all branches", Lines: lines, Error: strings.TrimSpace(g.err), Width: w}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}
