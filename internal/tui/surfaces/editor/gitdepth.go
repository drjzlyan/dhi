package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/textbuf"
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
	rev    string // F-140: a commit's patch for this file, read-only
	whole  bool   // F-162: rev's whole commit; each hunk's first line names its file
	dir    string // F-140: where to run git for rev
	// F-151: V marks a line range inside the hunk (anchor..row, body
	// lines counted from the line after @@); s/u then act on those lines.
	lineMode    bool
	row, anchor int
}

// hunkBody is the current hunk's lines after its @@ header.
func (d *diffView) hunkBody() []string {
	ls := strings.Split(strings.TrimRight(d.hunks[d.cur], "\n"), "\n")
	return ls[1:]
}

// selection is the changed (+/-) body lines between anchor and row.
func (d *diffView) selection() map[int]bool {
	body := d.hunkBody()
	lo, hi := min(d.anchor, d.row), max(d.anchor, d.row)
	sel := map[int]bool{}
	for i := lo; i <= hi && i < len(body); i++ {
		if strings.HasPrefix(body[i], "+") || strings.HasPrefix(body[i], "-") {
			sel[i] = true
		}
	}
	return sel
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
	if d.rev != "" {
		m.loadCommitDiff(d)
		return
	}
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
	if d.lineMode {
		switch key {
		case "esc", "q", "V":
			d.lineMode = false
			return true
		case "j", "down":
			d.row = min(d.row+1, max(len(d.hunkBody())-1, 0))
			return true
		case "k", "up":
			d.row = max(d.row-1, 0)
			return true
		case "n", "p":
			d.lineMode = false // moving to another hunk ends the line range
		}
	}
	switch key {
	case "esc", "q":
		m.diff = nil
		m.refreshGitData()
	case "j", "down", "n":
		d.cur = min(d.cur+1, max(len(d.hunks)-1, 0))
	case "k", "up", "p":
		d.cur = max(d.cur-1, 0)
	case "V": // F-151: mark lines inside this hunk
		if d.rev != "" {
			d.err = "a past commit is read-only"
			return true
		}
		if len(d.hunks) == 0 {
			return true
		}
		if d.note == "untracked" {
			d.err = "a new file stages whole — s"
			return true
		}
		first := 0
		for i, l := range d.hunkBody() {
			if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
				first = i
				break
			}
		}
		d.lineMode, d.row, d.anchor, d.err = true, first, first, ""
	case "s", "u": // stage (work tree) / unstage (index) this hunk
		if d.rev != "" {
			d.err = "a past commit is read-only"
			return true
		}
		if len(d.hunks) == 0 {
			return true
		}
		if (key == "s") == d.staged {
			d.err = map[bool]string{true: "this is the staged side — u unstages", false: "not staged yet — s stages"}[d.staged]
			return true
		}
		root := m.gitRepo.Path()
		var err error
		if d.lineMode {
			var patch string
			patch, err = gitcore.PartialPatch(d.head, d.hunks[d.cur], d.selection(), d.staged)
			if err == nil {
				err = m.gitApplyPartial(root, patch, d.staged)
			}
			d.lineMode = false
		} else if d.note == "untracked" {
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

// gitApplyPartial applies a line-level patch to the index (F-151).
func (m *Model) gitApplyPartial(dir, patch string, reverse bool) error {
	if m.gitCLI == nil {
		return fmt.Errorf("git is not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return m.gitCLI.ApplyPartial(ctx, dir, patch, reverse)
}

func (m *Model) diffView(base string) string {
	d := m.diff
	side := "changes"
	keys := "s stage this hunk · V lines"
	if d.staged {
		side, keys = "staged changes", "u unstage this hunk · V lines"
	}
	if d.lineMode {
		keys = map[bool]string{true: "u unstage", false: "s stage"}[d.staged] + " the marked lines · j/k extend · V/esc cancel"
	}
	if d.rev != "" {
		side, keys = "at "+shortHash(d.rev), "read-only"
	}
	lines := []string{theme.TextMuted().Render(fmt.Sprintf("%s · %s · hunk %d of %d", d.path, side, d.cur+1, max(len(d.hunks), 1))), ""}
	if len(d.hunks) == 0 {
		lines = append(lines, theme.TextDim().Render("no changes"))
	} else {
		sel := map[int]bool{}
		if d.lineMode {
			sel = d.selection()
		}
		var hunk []string
		for i, l := range strings.Split(strings.TrimRight(d.hunks[d.cur], "\n"), "\n") {
			l = expandTabs(l)
			bi := i - 1 // body index; the @@ line is -1
			pre := ""
			if d.lineMode { // a two-column gutter holds the cursor
				pre = "  "
				if bi == d.row {
					pre = theme.GlyphCursor + " "
				}
			}
			switch {
			case strings.HasPrefix(l, "@@") || (d.whole && i == 0):
				hunk = append(hunk, pre+theme.Hint().Render(l))
			case strings.HasPrefix(l, "+"):
				w := theme.AddWash()
				if sel[bi] {
					w = theme.AddWashStrong()
				}
				hunk = append(hunk, pre+w.Render(l))
			case strings.HasPrefix(l, "-"):
				w := theme.DelWash()
				if sel[bi] {
					w = theme.DelWashStrong()
				}
				hunk = append(hunk, pre+w.Render(l))
			default:
				hunk = append(hunk, pre+theme.TextDim().Render(l))
			}
		}
		// in line mode a long hunk scrolls to keep the cursor in view
		room := max(m.height-4, 8) - 4 - len(lines)
		if d.lineMode && len(hunk) > room {
			hunk = hunk[min(max(d.row+3-room, 0), len(hunk)-room):]
		}
		lines = append(lines, hunk...)
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

// commitMarkerNote explains {ai:…} markers left in the draft.
const commitMarkerNote = "# Parts like {ai:summary} are written by your default agent from the staged\n" +
	"# changes when you :w; replace one with your own text to write it yourself.\n"

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
	text := commitTemplate
	if m.commitConv.Draft != nil {
		if d := m.commitConv.Draft(m.gitRepo.Path()); strings.TrimSpace(d) != "" {
			// F-182: the team's shape, its symbols filled in.
			text = d + "\n" + commitTemplate
			if len(conventions.Markers(d)) > 0 {
				text += commitMarkerNote
			}
		}
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
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
	if slots := conventions.Markers(msg); len(slots) > 0 {
		return m.writeCommitParts(msg)
	}
	return m.commitMessage(msg)
}

// writeCommitParts has the AI parts of msg written off the UI, then
// commits (onCommitWritten). The message buffer closes meanwhile.
func (m *Model) writeCommitParts(msg string) string {
	if m.commitConv.Write == nil || m.termMsgs == nil {
		return "the message has {ai:…} parts and no agent can write them here — write them yourself"
	}
	if m.commitWriting {
		return "already writing a commit message"
	}
	if m.commitRepo == nil {
		return gitcore.ErrIdentityUnset.Error()
	}
	m.commitWriting = true
	dir, write, ch := m.commitRepo.Path(), m.commitConv.Write, m.termMsgs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, err := write(ctx, dir, msg)
		ev := teaMsg{kind: commitMsgWritten, note: out}
		if err != nil {
			ev.hoverText = err.Error()
		}
		select { // the call was paid for: wait for room rather than drop it
		case ch <- ev:
		case <-time.After(10 * time.Second):
		}
	}()
	return "writing the AI parts of the message with your agent…"
}

// onCommitWritten commits the written message, or names why not.
func (m *Model) onCommitWritten(msg teaMsg) {
	m.commitWriting = false
	if msg.hoverText != "" {
		m.gitMessage = "not committed: " + msg.hoverText
		m.commitRepo = nil
		return
	}
	m.gitMessage = m.commitMessage(msg.note)
}

// commitMessage commits msg in the commit buffer's repo.
func (m *Model) commitMessage(msg string) string {
	if m.commitRepo == nil || m.gitIdentity == nil {
		return gitcore.ErrIdentityUnset.Error()
	}
	return m.startCommit(m.commitRepo.Path(), msg, true)
}

// commitResult is a finished background commit.
type commitResult struct {
	path   string
	hash   string
	err    string
	buffer bool // from the message buffer (else the panel's commit line)
}

// startCommit commits what is staged in the repo at path off the UI
// goroutine: go-git reads the whole worktree status first, seconds on a
// big repo. Commits are authored by the user's resolved git identity
// (F-029). One at a time; commitMsgDone carries the outcome.
func (m *Model) startCommit(path, msg string, buffer bool) string {
	if m.committing {
		return "already committing — wait for it to finish"
	}
	if m.gitWriting {
		return "git is still staging — try again in a moment"
	}
	m.committing = true
	identity, ch := m.gitIdentity, m.termMsgs
	go func() {
		r := &commitResult{path: path, buffer: buffer}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		id, err := identity(ctx)
		if err == nil {
			var rp *gitcore.Repo
			if rp, err = gitcore.Open(path); err == nil {
				r.hash, err = rp.Commit(gitcore.CommitOptions{Message: msg, Author: id.Name, Email: id.Email})
			}
		}
		if err != nil {
			r.err = err.Error()
		}
		ch <- teaMsg{kind: commitMsgDone, commit: r}
	}()
	return "committing…"
}

// applyCommit reports a finished commit and refreshes what HEAD moving
// changed: the panel, the tree's letters and the gutters.
func (m *Model) applyCommit(r *commitResult) {
	if r == nil {
		return
	}
	m.committing = false
	if r.err != "" {
		if r.buffer {
			m.gitMessage = r.err
		} else {
			m.gitErr = r.err
		}
		m.refreshGitData()
		return
	}
	if r.buffer && m.commitRepo != nil && m.commitRepo.Path() == r.path {
		m.commitRepo = nil
	}
	m.gitErr = ""
	m.gitMessage = "committed " + shortHash(r.hash)
	m.invalidateGutters()
	m.invalidateTreeGit()
	m.refreshRows()
	m.refreshGitData()
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
			_, _ = fmt.Sscan(strings.TrimPrefix(ln, "author-time "), &sec)
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
