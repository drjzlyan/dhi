package editor

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// Git panel state (lazygit-style bottom panel over go-git, ADR-0008).

// ToggleGitPanel opens/closes/focus-blurs the git panel (ctrl+j),
// mirroring the terminal drawer cycle.
func (m *Model) ToggleGitPanel() {
	switch {
	case !m.gitOpen:
		m.gitOpen = true
		m.gitFocus = true
		m.loadGit()
	case m.gitFocus:
		m.gitFocus = false
	default:
		m.gitOpen = false
		m.gitFocus = false
	}
}

func (m *Model) loadGit() {
	root := m.currentGitRoot()
	if root == "" || !gitcore.IsRepo(root) {
		m.gitRepo = nil
		m.gitErr = "no git repository under this workspace"
		return
	}
	rp, err := gitcore.Open(root)
	if err != nil {
		m.gitRepo = nil
		m.gitErr = err.Error()
		return
	}
	if m.gitRepo == nil || m.gitRepo.Path() != rp.Path() {
		// Another repo: its rows must not show under this one's name.
		m.gitEntries, m.gitLog, m.gitConflicts = nil, nil, nil
	}
	m.gitRepo = rp
	m.gitErr = ""
	m.refreshGitData()
}

// gitDataResult is a background read of the panel's status, log and
// conflicts for the repo at path.
type gitDataResult struct {
	path      string
	gen       int
	entries   []gitcore.FileStatus
	log       []gitcore.CommitEntry
	conflicts map[string]string
	err       string
}

// refreshGitData re-reads the panel off the UI goroutine: go-git status
// hashes the worktree and the conflict list runs git, seconds on a big
// repo. The panel keeps its rows until gitDataMsgDone lands. The read
// opens its own handle, so it never shares go-git state with a stage or
// commit on the UI goroutine.
func (m *Model) refreshGitData() {
	if m.gitRepo == nil || m.termMsgs == nil {
		return
	}
	m.gitDataGen++
	m.gitLoading = true
	path, gen, cli, ch := m.gitRepo.Path(), m.gitDataGen, m.gitCLI, m.termMsgs
	go func() {
		r := &gitDataResult{path: path, gen: gen}
		rp, err := gitcore.Open(path)
		if err != nil {
			r.err = err.Error()
			ch <- teaMsg{kind: gitDataMsgDone, gitData: r}
			return
		}
		if st, err := rp.Status(); err == nil {
			r.entries = st
		} else {
			r.err = err.Error()
		}
		if log, err := rp.Log(15); err == nil {
			r.log = log
		}
		if cli != nil { // F-156
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			list, err := gitcore.Conflicts(ctx, cli, path)
			cancel()
			if err != nil {
				r.err = err.Error()
			} else if len(list) > 0 {
				r.conflicts = make(map[string]string, len(list))
				for _, c := range list {
					r.conflicts[c.Path] = c.Code
				}
			}
		}
		ch <- teaMsg{kind: gitDataMsgDone, gitData: r}
	}()
}

// applyGitData shows the latest read for the open repo; an older one, or
// one for a repo the panel has left, is dropped.
func (m *Model) applyGitData(r *gitDataResult) {
	if r == nil || r.gen != m.gitDataGen || m.gitRepo == nil || m.gitRepo.Path() != r.path {
		return
	}
	m.gitLoading = false
	m.gitEntries, m.gitConflicts = r.entries, r.conflicts
	if r.log != nil {
		m.gitLog = r.log
	}
	if r.err != "" {
		m.gitErr = r.err
	}
	if n := m.gitRowCount(); m.gitCursor >= n {
		m.gitCursor = max(n-1, 0)
	}
}

// currentGitRoot picks the active buffer's repo, else the first member
// that is a repository.
func (m *Model) currentGitRoot() string {
	if e := m.active(); e != nil {
		for _, mem := range m.members {
			if len(e.Path()) > len(mem.path) && e.Path()[:len(mem.path)] == mem.path {
				return mem.path
			}
		}
	}
	for _, mem := range m.members {
		if info, err := os.Stat(mem.path); err == nil && info.IsDir() && gitcore.IsRepo(mem.path) {
			return mem.path
		}
	}
	return ""
}

func (m *Model) handleGitKey(key string) bool {
	if m.gitInputMode {
		return m.handleCommitInput(key)
	}

	switch key {
	case "esc":
		m.gitFocus = false
		return true
	case "tab":
		m.gitTab = (m.gitTab + 1) % 2
		m.gitCursor = 0
		return true
	}

	switch key {
	case "j", "down":
		if m.gitCursor < m.gitRowCount()-1 {
			m.gitCursor++
		}
		return true
	case "k", "up":
		if m.gitCursor > 0 {
			m.gitCursor--
		}
		return true
	}

	if m.gitTab == 0 { // status actions
		switch key {
		case "s":
			m.stageAt(m.gitSelectedPath(), false)
			return true
		case "S":
			m.stageAt(".", true)
			return true
		case "u":
			m.unstageAt()
			return true
		case "c":
			m.gitInputMode = true
			m.gitInput = nil
			return true
		case "enter", "d": // F-121: the file's diff, hunk by hunk
			m.openDiff()
			return true
		case "C": // F-121: a multi-line message in a buffer
			m.openCommitBuffer()
			return true
		}
	}
	switch key { // F-121, either tab
	case "b":
		m.openBranches()
		return true
	case "z": // F-129
		m.openStash()
		return true
	case "h": // F-140: the file's commits (the selected change, else the open buffer)
		abs := ""
		if rel := m.gitSelectedPath(); rel != "" && m.gitRepo != nil {
			abs = filepath.Join(m.gitRepo.Path(), rel)
		}
		if msg := m.openHistory(abs); msg != "" {
			m.gitErr = msg
		}
		return true
	case "g": // F-162: the branch graph
		if msg := m.openGraph(); msg != "" {
			m.gitErr = msg
		}
		return true
	case "p":
		m.pushPull(false)
		return true
	case "P":
		m.pushPull(true)
		return true
	}
	return true // panel swallows everything while focused
}

func (m *Model) handleCommitInput(key string) bool {
	switch key {
	case "esc":
		m.gitInputMode = false
		m.gitInput = nil
		return true
	case "enter":
		msg := strings.TrimSpace(string(m.gitInput))
		m.gitInputMode = false
		if msg == "" || m.gitRepo == nil {
			return true
		}
		// F-029: commits are authored by the user's resolved git
		// identity; a missing resolver or an unset identity refuses
		// with the named fix, never a synthetic author.
		if m.gitIdentity == nil {
			m.gitErr = gitcore.ErrIdentityUnset.Error()
			return true
		}
		m.gitMessage = m.startCommit(m.gitRepo.Path(), msg, false)
		return true
	}
	m.gitInput, _ = kit.EditRunes(m.gitInput, key)
	return true
}

func (m *Model) stageAt(path string, all bool) {
	if m.gitRepo == nil {
		return
	}
	var paths []string
	if all || path == "" {
		paths = []string{"."}
	} else {
		paths = []string{path}
	}
	if m.gitConflicts[path] != "" && m.gitCLI != nil { // F-156: go-git cannot resolve an unmerged entry
		cli := m.gitCLI
		m.gitWrite(func(dir string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return gitcore.StagePath(ctx, cli, dir, path)
		})
		return
	}
	m.gitWrite(func(dir string) error {
		rp, err := gitcore.Open(dir)
		if err != nil {
			return err
		}
		return rp.Stage(paths...)
	})
}

func (m *Model) unstageAt() {
	path := m.gitSelectedPath()
	if m.gitRepo == nil || path == "" {
		return
	}
	m.gitWrite(func(dir string) error {
		rp, err := gitcore.Open(dir)
		if err != nil {
			return err
		}
		return rp.Unstage(path)
	})
}

// gitWrite runs one index write (stage, unstage) in the panel's repo off
// the UI goroutine: go-git reads the worktree status for each, seconds on
// a big repo. Writes run one at a time, and never beside a commit, so the
// index changes in the order the keys asked; gitOpMsgDone refreshes.
func (m *Model) gitWrite(op func(dir string) error) {
	if m.gitWriting || m.committing {
		m.gitMessage = "git is still busy — try again in a moment"
		return
	}
	m.gitWriting = true
	dir, ch := m.gitRepo.Path(), m.termMsgs
	go func() {
		err := op(dir)
		msg := teaMsg{kind: gitOpMsgDone}
		if err != nil {
			msg.hoverText = err.Error()
		}
		ch <- msg
	}()
}

// applyGitWrite reports a finished index write and re-reads the panel.
func (m *Model) applyGitWrite(msg teaMsg) {
	m.gitWriting = false
	if msg.hoverText != "" {
		m.gitErr = msg.hoverText
	}
	m.refreshGitData()
}

// selection model: rows are [staged files..., unstaged files...] flat;
// headers are not selectable.
func (m *Model) gitStagedFiles() []FileEntry {
	var out []FileEntry
	for _, f := range m.gitEntries {
		if f.Staged && m.gitConflicts[f.Path] == "" {
			out = append(out, FileEntry{Path: f.Path, Letter: string(f.X)})
		}
	}
	return out
}

type FileEntry struct {
	Path   string
	Letter string
	// Conflict marks an unmerged file (F-156); Letter is its code, "UU".
	Conflict bool
}

// gitUnstagedFiles lists the work-tree changes, unmerged files first.
func (m *Model) gitUnstagedFiles() []FileEntry {
	var out []FileEntry
	paths := make([]string, 0, len(m.gitConflicts))
	for p := range m.gitConflicts {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths { // from the user's tool: go-git's status does not see them
		out = append(out, FileEntry{Path: p, Letter: m.gitConflicts[p], Conflict: true})
	}
	for _, f := range m.gitEntries {
		if m.gitConflicts[f.Path] != "" {
			continue
		}
		if (!f.Staged && f.WorktreeDirty()) || f.Y == '?' || f.Y == 'U' {
			out = append(out, FileEntry{Path: f.Path, Letter: string(f.Y)})
		}
	}
	return out
}

func (m *Model) gitRowCount() int {
	if m.gitTab == 0 {
		return len(m.gitStagedFiles()) + len(m.gitUnstagedFiles())
	}
	return len(m.gitLog)
}

func (m *Model) gitSelectedPath() string {
	staged := m.gitStagedFiles()
	if m.gitCursor < len(staged) {
		return staged[m.gitCursor].Path
	}
	unstaged := m.gitUnstagedFiles()
	i := m.gitCursor - len(staged)
	if i >= 0 && i < len(unstaged) {
		return unstaged[i].Path
	}
	return ""
}

// gitPanelView renders the bottom git panel.
func (m *Model) gitPanelView() string {
	h := min(gitPanelHeight, maxInt(m.height/3, 5))
	body := m.gitBody(h)
	panel := kit.NewPanel("git"+m.gitRepoLabel(), m.gitFocus)
	panel.SetContent(body...)
	panel.Width = maxInt(m.width, 20)
	panel.Height = h
	return panel.View()
}

func (m *Model) gitRepoLabel() string {
	if m.gitErr != "" {
		return ""
	}
	if m.gitRepo == nil {
		return ""
	}
	br := "main"
	if b, err := m.gitRepo.CurrentBranch(); err == nil && b != "" {
		br = b
	}
	short := m.gitRepo.Path()
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	return "  " + theme.TextStyle().Render(short) + " " + kit.Pill(br, theme.Current.TextDim) + " "
}

func (m *Model) gitBody(h int) []string {
	var rows []string
	rows = append(rows, kit.TabPills([]string{"status", "log"}, m.gitTab))
	cursorRow := -1 // where the cursor's row lands, to keep it in view (F-121)

	switch {
	case m.gitErr != "":
		rows = append(rows, "", theme.DangerText().Render(m.gitErr))
	case m.gitTab == 0:
		staged := m.gitStagedFiles()
		unstaged := m.gitUnstagedFiles()
		idx := 0
		addSection := func(header string, files []FileEntry, isStaged bool) {
			rows = append(rows, theme.SectionHeader().Render(strings.ToUpper(header))+
				" "+theme.TextMuted().Render(strconv.Itoa(len(files))))
			for _, f := range files {
				marker := " "
				if idx == m.gitCursor {
					marker = theme.GlyphCursor
					cursorRow = len(rows)
				}
				letter := theme.WarningText().Render(f.Letter + " ")
				if f.Conflict {
					letter = theme.DangerText().Render(f.Letter + " ")
				}
				if isStaged {
					letter = theme.SuccessText().Render(f.Letter + " ")
				}
				path := f.Path
				if idx == m.gitCursor {
					path = theme.TabActive().Render(path)
				} else {
					path = theme.TextDim().Render(path)
				}
				rows = append(rows, marker+letter+path)
				idx++
			}
			if len(files) == 0 && isStaged {
				rows = append(rows, theme.TextDim().Render("  (nothing staged)"))
			}
		}
		if m.gitLoading && m.gitEntries == nil {
			rows = append(rows, theme.TextDim().Render("  reading git status…"))
			break
		}
		addSection(stagedHeader, staged, true)
		addSection(unstagedHeader, unstaged, false)

		if m.gitInputMode {
			rows = append(rows, theme.Brand().Render("commit: "+string(m.gitInput)+"▌"))
		}
	default:
		if len(m.gitLog) == 0 {
			rows = append(rows, "", theme.TextDim().Render("no commits yet"))
		}
		for _, c := range m.gitLog {
			line := theme.Hint().Render(c.Short) + " " + c.Message +
				"  " + theme.TextDim().Render(c.Author)
			rows = append(rows, line)
		}
	}

	if msg := m.gitMessage; msg != "" {
		rows = append(rows, theme.SuccessText().Render(msg))
	}
	if room := h - 3; len(rows) > room && cursorRow >= room-1 {
		// Scroll: keep the tab pills, then the window that holds the cursor.
		start := min(cursorRow-(room-2), len(rows)-(room-1))
		rows = append([]string{rows[0]}, rows[start:start+room-1]...)
	}
	for len(rows) < h-3 {
		rows = append(rows, "")
	}
	if len(rows) > h-3 {
		rows = rows[:h-3]
	}
	return rows
}

const (
	stagedHeader   = "staged"
	unstagedHeader = "unstaged"
)
