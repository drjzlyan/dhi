package editor

import (
	"context"
	"fmt"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Git depth (F-121): a file's diff with hunk staging, branches, push and
// pull in a terminal tab you watch, a multi-line commit message in a
// buffer, and blame for the cursor line. Everything runs the user's own
// git (ADR-0044).

// WithGit attaches the user's git CLI.
func WithGit(r *gitcore.Runner) Option { return func(m *Model) { m.gitCLI = r } }

func (m *Model) gitRun(dir string, args ...string) (string, error) {
	return m.gitRunInput(dir, "", args...)
}

func (m *Model) gitRunInput(dir, stdin string, args ...string) (string, error) {
	if m.gitCLI == nil {
		return "", fmt.Errorf("git is not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, _, err := m.gitCLI.RunInput(ctx, dir, stdin, args...)
	return out, err
}

// ---- diff and hunks ----

type diffView struct {
	path   string // repo-relative
	staged bool   // the index side (unstage hunks) or the work tree (stage)
	head   string // the patch's file header
	hunks  []string
	cur    int
	err    string
	note   string
}

// splitPatch cuts one file's patch into its header and raw hunks.
func splitPatch(patch string) (head string, hunks []string) {
	lines := strings.SplitAfter(patch, "\n")
	var cur strings.Builder
	inHunk := false
	for _, l := range lines {
		if strings.HasPrefix(l, "@@") {
			if inHunk {
				hunks = append(hunks, cur.String())
			} else {
				head = cur.String()
			}
			cur.Reset()
			inHunk = true
		}
		cur.WriteString(l)
	}
	if inHunk {
		hunks = append(hunks, cur.String())
	} else {
		head = cur.String()
	}
	return head, hunks
}

// openDiff shows the selected file's changes (enter in the git panel).
func (m *Model) openDiff() {
	path := m.gitSelectedPath()
	if path == "" || m.gitRepo == nil {
		return
	}
	staged := m.gitCursor < len(m.gitStagedFiles())
	d := &diffView{path: path, staged: staged}
	m.loadDiff(d)
	m.diff = d
}

func (m *Model) loadDiff(d *diffView) {
	root := m.gitRepo.Path()
	args := []string{"diff", "--no-color", "--no-ext-diff"}
	if d.staged {
		args = append(args, "--cached")
	}
	out, err := m.gitRun(root, append(args, "--", d.path)...)
	if err != nil {
		d.err = err.Error()
		return
	}
	if strings.TrimSpace(out) == "" && !d.staged { // untracked: all of it is new
		if data, rerr := os.ReadFile(filepath.Join(root, d.path)); rerr == nil {
			d.head, d.hunks = "", []string{"(new file — s stages it whole)\n" + prefixLines(string(data), "+")}
			d.note = "untracked"
			return
		}
	}
	d.head, d.hunks = splitPatch(out)
	d.cur = min(d.cur, max(len(d.hunks)-1, 0))
}

func prefixLines(s, p string) string {
	var b strings.Builder
	for _, l := range strings.SplitAfter(s, "\n") {
		if l != "" {
			b.WriteString(p + l)
		}
	}
	return b.String()
}

func (m *Model) diffKey(key string) bool {
	d := m.diff
	switch key {
	case "esc", "q":
		m.diff = nil
		m.refreshGitData()
	case "j", "down", "n":
		d.cur = min(d.cur+1, max(len(d.hunks)-1, 0))
	case "k", "up", "p":
		d.cur = max(d.cur-1, 0)
	case "s", "u": // stage (work tree) / unstage (index) this hunk
		if len(d.hunks) == 0 {
			return true
		}
		if (key == "s") == d.staged {
			d.err = map[bool]string{true: "this is the staged side — u unstages", false: "not staged yet — s stages"}[d.staged]
			return true
		}
		root := m.gitRepo.Path()
		var err error
		if d.note == "untracked" {
			_, err = m.gitRun(root, "add", "--", d.path)
		} else {
			args := []string{"apply", "--cached", "--recount"}
			if d.staged {
				args = append(args, "-R")
			}
			_, err = m.gitRunInput(root, d.head+d.hunks[d.cur], append(args, "-")...)
		}
		if err != nil {
			d.err = err.Error()
			return true
		}
		d.err = ""
		m.invalidateGutters()
		m.loadDiff(d)
		if len(d.hunks) == 0 {
			m.diff = nil
			m.refreshGitData()
			m.gitMessage = map[bool]string{true: "unstaged", false: "staged"}[d.staged] + " " + d.path
		}
	}
	return true
}

func (m *Model) diffView(base string) string {
	d := m.diff
	side := "changes"
	keys := "s stage this hunk"
	if d.staged {
		side, keys = "staged changes", "u unstage this hunk"
	}
	lines := []string{theme.TextMuted().Render(fmt.Sprintf("%s · %s · hunk %d of %d", d.path, side, d.cur+1, max(len(d.hunks), 1))), ""}
	if len(d.hunks) == 0 {
		lines = append(lines, theme.TextDim().Render("no changes"))
	} else {
		for _, l := range strings.Split(strings.TrimRight(d.hunks[d.cur], "\n"), "\n") {
			l = expandTabs(l)
			switch {
			case strings.HasPrefix(l, "@@"):
				lines = append(lines, theme.Hint().Render(l))
			case strings.HasPrefix(l, "+"):
				lines = append(lines, theme.AddWash().Render(l))
			case strings.HasPrefix(l, "-"):
				lines = append(lines, theme.DelWash().Render(l))
			default:
				lines = append(lines, theme.TextDim().Render(l))
			}
		}
	}
	h := max(m.height-4, 8)
	if len(lines) > h-4 {
		lines = append(lines[:h-5], theme.TextDim().Render("…"))
	}
	lines = append(lines, "", theme.Hint().Render(keys+" · j/k hunk · esc back"))
	md := kit.Modal{Title: "diff", Lines: lines, Error: d.err, Width: min(m.width-4, 110)}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}

// ---- branches ----

type branchPicker struct {
	all   []string
	cur   string
	input kit.Input
	sel   int
	err   string
}

func (m *Model) openBranches() {
	if m.gitRepo == nil {
		return
	}
	out, err := m.gitRun(m.gitRepo.Path(), "for-each-ref", "--sort=-committerdate", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		m.gitErr = err.Error()
		return
	}
	cur, _ := m.gitRepo.CurrentBranch()
	m.branches = &branchPicker{all: strings.Fields(out), cur: cur, input: kit.NewInput("", "filter, or a new branch name")}
}

func (b *branchPicker) matches() []string {
	q := strings.ToLower(strings.TrimSpace(b.input.Value))
	var out []string
	for _, n := range b.all {
		if q == "" || strings.Contains(strings.ToLower(n), q) {
			out = append(out, n)
		}
	}
	return out
}

func (m *Model) branchKey(key string) bool {
	b := m.branches
	ms := b.matches()
	switch key {
	case "esc":
		m.branches = nil
		return true
	case "down", "ctrl+n":
		b.sel = min(b.sel+1, max(len(ms)-1, 0))
		return true
	case "up", "ctrl+p":
		b.sel = max(b.sel-1, 0)
		return true
	case "enter":
		root := m.gitRepo.Path()
		var args []string
		var what string
		if len(ms) > 0 {
			args, what = []string{"switch", ms[min(b.sel, len(ms)-1)]}, "switched to "+ms[min(b.sel, len(ms)-1)]
		} else if name := strings.TrimSpace(b.input.Value); name != "" {
			args, what = []string{"switch", "-c", name}, "created and switched to "+name
		} else {
			return true
		}
		if _, err := m.gitRun(root, args...); err != nil {
			b.err = err.Error()
			return true
		}
		m.branches = nil
		m.gitMessage = what
		m.invalidateGutters()
		m.invalidateTreeGit()
		m.refreshGitData()
		m.checkDisk() // buffers follow the branch's files
		m.refreshTree()
		return true
	}
	b.input.HandleKey(key)
	b.sel = 0
	return true
}

func (m *Model) branchView(base string) string {
	b := m.branches
	lines := append([]string{}, b.input.View(56)...)
	ms := b.matches()
	if len(ms) == 0 && strings.TrimSpace(b.input.Value) != "" {
		lines = append(lines, theme.TextMuted().Render("enter creates "+strings.TrimSpace(b.input.Value)+" from here"))
	}
	for i, n := range ms {
		if i == 12 {
			break
		}
		label := n
		if n == b.cur {
			label += "  (current)"
		}
		if i == b.sel {
			lines = append(lines, theme.GlyphCursor+" "+theme.TabActive().Render(label))
		} else {
			lines = append(lines, "  "+theme.TextDim().Render(label))
		}
	}
	lines = append(lines, "", theme.Hint().Render("enter switch · ↑/↓ move · esc cancel"))
	md := kit.Modal{Title: "branches", Lines: lines, Error: b.err, Width: 64}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}

// ---- push and pull: in a terminal tab, so prompts and output are yours ----

func (m *Model) pushPull(push bool) {
	if m.gitRepo == nil || m.gitCLI == nil {
		return
	}
	root := m.gitRepo.Path()
	argv := []string{"git", "pull", "--ff-only"}
	if push {
		argv = []string{"git", "push"}
		if _, err := m.gitRun(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
			if br, berr := m.gitRepo.CurrentBranch(); berr == nil && br != "" {
				argv = []string{"git", "push", "-u", "origin", br} // first push: set the upstream
			}
		}
	}
	m.runInTab(strings.Join(argv[:2], " "), root, argv)
	m.gitFocus = false
}

// ---- multi-line commit in a buffer ----

const commitTemplate = "\n# Write the commit message above: a subject line, a blank line, then\n" +
	"# the body. Lines starting with # are dropped. :w commits, :q! cancels.\n"

// commitPath is the message file for the panel's repo.
func (m *Model) commitPath() string {
	return filepath.Join(m.ws.Root, workspace.DHIDir, "tmp", "COMMIT_EDITMSG")
}

func (m *Model) openCommitBuffer() {
	if m.gitRepo == nil {
		return
	}
	p := m.commitPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(commitTemplate), 0o644); err != nil {
		m.gitErr = err.Error()
		return
	}
	m.commitRepo = m.gitRepo
	m.gitFocus = false
	m.open(&node{kind: nodeFile, path: p, name: "COMMIT_EDITMSG"})
	if e := m.active(); e != nil {
		e.Buffer().SetCursor(textbuf.Pos{})
		e.Key("i")
		e.SetMessage("commit message for " + filepath.Base(m.commitRepo.Path()) + " — :w commits")
	}
}

// commitFromBuffer runs after the message buffer is saved.
func (m *Model) commitFromBuffer(text string) string {
	var keep []string
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(l, "#") {
			keep = append(keep, l)
		}
	}
	msg := strings.TrimSpace(strings.Join(keep, "\n"))
	if msg == "" {
		return "empty message — nothing committed"
	}
	if m.commitRepo == nil || m.gitIdentity == nil {
		return gitcore.ErrIdentityUnset.Error()
	}
	id, err := m.gitIdentity(context.Background())
	if err != nil {
		return err.Error()
	}
	hash, err := m.commitRepo.Commit(gitcore.CommitOptions{Message: msg, Author: id.Name, Email: id.Email})
	if err != nil {
		return err.Error()
	}
	m.commitRepo = nil
	m.invalidateGutters()
	m.refreshGitData()
	return "committed " + shortHash(hash)
}

// ---- blame ----

// blameLine names who last changed the cursor line and when.
func (m *Model) blameLine() string {
	e := m.active()
	if e == nil {
		return "open a file first"
	}
	dir := filepath.Dir(e.Path())
	l := e.Buffer().Cursor().Line + 1
	out, err := m.gitRun(dir, "blame", "--porcelain", "-L", fmt.Sprintf("%d,%d", l, l), "--", filepath.Base(e.Path()))
	if err != nil {
		return "blame: " + err.Error()
	}
	var hash, author, summary string
	var when time.Time
	for i, ln := range strings.Split(out, "\n") {
		switch {
		case i == 0:
			if f := strings.Fields(ln); len(f) > 0 {
				hash = f[0]
			}
		case strings.HasPrefix(ln, "author "):
			author = strings.TrimPrefix(ln, "author ")
		case strings.HasPrefix(ln, "author-time "):
			var sec int64
			fmt.Sscan(strings.TrimPrefix(ln, "author-time "), &sec)
			when = time.Unix(sec, 0)
		case strings.HasPrefix(ln, "summary "):
			summary = strings.TrimPrefix(ln, "summary ")
		}
	}
	if strings.Trim(hash, "0") == "" {
		return "not committed yet"
	}
	return fmt.Sprintf("%s · %s · %s · %s", shortHash(hash), author, when.Format("2006-01-02"), summary)
}
