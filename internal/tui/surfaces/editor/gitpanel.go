package editor

import (
	"os"
	"strings"

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
	m.gitRepo = rp
	m.gitErr = ""
	m.refreshGitData()
}

func (m *Model) refreshGitData() {
	if m.gitRepo == nil {
		return
	}
	if st, err := m.gitRepo.Status(); err == nil {
		m.gitEntries = st
	} else {
		m.gitErr = err.Error()
	}
	if log, err := m.gitRepo.Log(15); err == nil {
		m.gitLog = log
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
		}
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
		hash, err := m.gitRepo.Commit(gitcore.CommitOptions{
			Message: msg, Author: "dhi", Email: "dhi@local",
		})
		if err != nil {
			m.gitErr = err.Error()
		} else {
			m.gitErr = ""
			m.gitMessage = "committed " + shortHash(hash)
		}
		m.refreshGitData()
		return true
	case "backspace":
		if len(m.gitInput) > 0 {
			m.gitInput = m.gitInput[:len(m.gitInput)-1]
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		m.gitInput = append(m.gitInput, r...)
		return true
	}
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
	if err := m.gitRepo.Stage(paths...); err != nil {
		m.gitErr = err.Error()
	}
	m.refreshGitData()
}

func (m *Model) unstageAt() {
	if m.gitRepo == nil || m.gitSelectedPath() == "" {
		return
	}
	if err := m.gitRepo.Unstage(m.gitSelectedPath()); err != nil {
		m.gitErr = err.Error()
	}
	m.refreshGitData()
}

// selection model: rows are [staged files..., unstaged files...] flat;
// headers are not selectable.
func (m *Model) gitStagedFiles() []FileEntry {
	var out []FileEntry
	for _, f := range m.gitEntries {
		if f.Staged {
			out = append(out, FileEntry{Path: f.Path, Letter: string(f.X)})
		}
	}
	return out
}

type FileEntry struct {
	Path   string
	Letter string
}

func (m *Model) gitUnstagedFiles() []FileEntry {
	var out []FileEntry
	for _, f := range m.gitEntries {
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
	panel := kit.NewPanel("git"+gitFocusMark(m.gitFocus)+m.gitRepoLabel(), false)
	panel.SetContent(body...)
	panel.Width = maxInt(m.width-railWidth-1, 20)
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
	return "  " + theme.Hint().Render(short+" · "+br+"  ")
}

func gitFocusMark(focus bool) string {
	if focus {
		return " " + theme.Brand().Render(theme.GlyphDot)
	}
	return ""
}

func (m *Model) gitBody(h int) []string {
	var rows []string
	tabs := theme.Hint().Render("[status]")
	if m.gitTab == 1 {
		tabs = "[log]"
	}
	rows = append(rows, tabs)

	switch {
	case m.gitErr != "":
		rows = append(rows, "", theme.DangerText().Render(m.gitErr))
	case m.gitTab == 0:
		staged := m.gitStagedFiles()
		unstaged := m.gitUnstagedFiles()
		idx := 0
		addSection := func(header string, files []FileEntry, isStaged bool) {
			rows = append(rows, theme.TabActive().Render(header))
			for _, f := range files {
				marker := " "
				if idx == m.gitCursor {
					marker = theme.GlyphCursor
				}
				letter := theme.WarningText().Render(f.Letter + " ")
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
	hint := theme.Hint().Render("s stage · S all · u unstage · c commit · tab status/log · esc blur")
	rows = append(rows, hint)
	for len(rows) < h-2 {
		rows = append(rows, "")
	}
	if len(rows) > h-2 {
		rows = rows[:h-2]
	}
	return rows
}

const (
	stagedHeader   = "staged"
	unstagedHeader = "unstaged"
)
