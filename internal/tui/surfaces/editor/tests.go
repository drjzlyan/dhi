package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/testrun"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Test runner (F-040). `:test` runs the current file's package, `:test
// all` the whole member, `:test <pattern>` only matching tests in the
// package. A green run is one status line; a red one opens a failures
// overlay where enter jumps to the failing line.

// testRunTimeout bounds one run so a hung test cannot pin the editor.
var testRunTimeout = 5 * time.Minute

type testState struct {
	rep     testrun.Report
	cur     int
	dir     string // where the run started (for rerun)
	args    []string
	running bool
}

// testCommand handles :test. It returns the status message and whether
// cmd was a test command.
func (m *Model) testCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "test" {
		return "", false
	}
	if m.testing != nil && m.testing.running {
		return "test: a run is already in progress", true
	}
	e := m.active()
	if e == nil {
		return "test: open a file first", true
	}
	root, _, ok := m.repoRel(e.Path())
	if !ok {
		return "test: the file is not inside a workspace member", true
	}
	if !strings.HasSuffix(e.Path(), ".go") {
		return "test: only Go packages are supported", true
	}
	dir, args := filepath.Dir(e.Path()), []string{"."}
	switch {
	case len(f) == 2 && f[1] == "all":
		dir, args = root, []string{"./..."}
	case len(f) >= 2:
		args = []string{"-run", strings.Join(f[1:], " "), "."}
	}
	m.startTests(dir, args)
	return "running tests…", true
}

func (m *Model) startTests(dir string, args []string) {
	st := &testState{dir: dir, args: args, running: true}
	m.testing = st
	env := m.termEnv
	ch := m.termMsgs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), testRunTimeout)
		defer cancel()
		rep := testrun.Run(ctx, dir, env, args...)
		ch <- teaMsg{kind: testMsgDone, testRep: &rep}
	}()
}

// applyTestDone folds a finished run into the model.
func (m *Model) applyTestDone(rep *testrun.Report) {
	if m.testing == nil || rep == nil {
		return
	}
	m.testing.running = false
	m.testing.rep = *rep
	m.testing.cur = 0
	e := m.active()
	switch {
	case rep.Err != "":
		if e != nil {
			e.SetMessage("test: " + rep.Err)
		}
	case len(rep.Failures) == 0:
		if e != nil {
			e.SetMessage(fmt.Sprintf("tests passed ✓%d (%s)", rep.Passed, rep.Elapsed.Round(10*time.Millisecond)))
		}
	default:
		m.mode = modeTests
	}
}

func (m *Model) handleTestKey(key string) bool {
	st := m.testing
	if st == nil {
		m.mode = modeNav
		return false
	}
	n := len(st.rep.Failures)
	switch key {
	case "esc", "q":
		m.mode = modeNav
	case "down", "j":
		if st.cur < n-1 {
			st.cur++
		}
	case "up", "k":
		if st.cur > 0 {
			st.cur--
		}
	case "r":
		m.mode = modeNav
		m.startTests(st.dir, st.args)
		if e := m.active(); e != nil {
			e.SetMessage("running tests…")
		}
	case "enter":
		if st.cur < n {
			m.jumpToFailure(st.rep.Failures[st.cur])
		}
	}
	return true
}

func (m *Model) jumpToFailure(f testrun.Failure) {
	if f.File == "" || !filepath.IsAbs(f.File) {
		if e := m.active(); e != nil {
			e.SetMessage("test: the failure has no file location")
		}
		m.mode = modeNav
		return
	}
	if m.OpenPaths(f.File) == 0 && m.tabFor(f.File) == nil {
		if e := m.active(); e != nil {
			e.SetMessage("test: cannot open " + f.File)
		}
		m.mode = modeNav
		return
	}
	for i, t := range m.bufs {
		if t.path == f.File {
			m.activeTab = i
		}
	}
	if e := m.active(); e != nil && f.Line > 0 {
		e.Buffer().SetCursor(textbuf.Pos{Line: f.Line - 1, Col: 0})
	}
	m.bufFocus = true
	m.mode = modeNav
}

func (m *Model) testsView() string {
	st := m.testing
	rep := st.rep
	head := fmt.Sprintf("%s %d passed · %s %d failed",
		theme.SuccessText().Render(theme.GlyphCheck), rep.Passed,
		theme.DangerText().Render(theme.GlyphCross), rep.Failed)
	if rep.Skipped > 0 {
		head += fmt.Sprintf(" · %d skipped", rep.Skipped)
	}
	body := []string{head, ""}
	for i, f := range rep.Failures {
		name := f.Package
		if f.Test != "" {
			name = f.Test
		} else {
			name += " (build)"
		}
		loc := ""
		if f.File != "" {
			loc = fmt.Sprintf("%s:%d", m.shortPath(f.File), f.Line)
		}
		row := fmt.Sprintf("%s %s", theme.GlyphCross, name)
		if i == st.cur {
			body = append(body, theme.Chip().Render(" "+row+" "))
		} else {
			body = append(body, theme.DangerText().Render(row))
		}
		if loc != "" || f.Message != "" {
			body = append(body, "    "+theme.TextDim().Render(truncateRunes(strings.TrimSpace(loc+"  "+f.Message), 74)))
		}
	}
	box := kit.NewPanel("test failures", true)
	box.SetContent(body...)
	box.Width = 82
	box.Height = min(len(body)+2, m.height)
	hint := theme.Hint().Render("enter jump · j/k move · r rerun · esc close")
	return kit.Center(joinV(box.View(), "", hint), m.width, m.height)
}

// shortPath shows a path relative to its member (or as given).
func (m *Model) shortPath(abs string) string {
	if _, rel, ok := m.repoRel(abs); ok {
		return rel
	}
	return abs
}
