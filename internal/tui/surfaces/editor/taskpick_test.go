package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// echoTools resolves every tool to /bin/echo so a "task" runs harmlessly.
type echoTools struct{}

func (echoTools) Find(string) (string, bool) { return "/bin/echo", true }
func (echoTools) Plan(tool string) ToolPlan  { return ToolPlan{Tool: tool} }
func (echoTools) Refresh()                   {}

type noTools struct{ echoTools }

func (noTools) Find(string) (string, bool) { return "", false }

func taskEditor(t *testing.T, tools HostTools) (*Model, string) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, root := setupWorkspace(t)
	alpha := filepath.Join(root, "repos", "alpha")
	if err := os.WriteFile(filepath.Join(alpha, "package.json"), []byte(`{"scripts":{"build":"tsc","lint":"eslint ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alpha, "Makefile"), []byte("deploy:\n\techo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New("test", ws, WithHostTools(tools), WithTermEnv(os.Environ()))
	m.Resize(100, 30)
	m.open(&node{kind: nodeFile, path: filepath.Join(alpha, "app.go"), name: "app.go"})
	return m, alpha
}

// F-146: :task lists the project's own tasks, filters, and runs the pick
// in a terminal tab in that member's folder.
func TestTaskPickerListsFiltersAndRunsInTheMemberFolder(t *testing.T) {
	m, alpha := taskEditor(t, &echoTools{})
	m.ExecEx(m.active(), "task")
	if m.mode != modeTasks {
		t.Fatalf("mode = %v, msg %q", m.mode, m.active().Message())
	}
	v := plainView(m)
	for _, want := range []string{"build", "lint", "deploy", "alpha"} {
		if !strings.Contains(v, want) {
			t.Errorf("picker misses %q:\n%s", want, v)
		}
	}
	typeKeys(m, "dep")
	if got := m.taskPick.Matches(); len(got) != 1 || got[0].Title != "deploy" {
		t.Fatalf("filter dep = %+v", got)
	}
	feed(m, "enter")
	if m.mode != modeNav || len(m.terms) != 1 {
		t.Fatalf("mode=%v terms=%d", m.mode, len(m.terms))
	}
	tab := m.terms[0]
	if tab.dir != alpha || tab.task == nil || tab.task.name != "deploy" || !m.drawerOpen {
		t.Errorf("tab = %+v drawer=%v", tab, m.drawerOpen)
	}
}

func TestTaskByNameAndMissingTool(t *testing.T) {
	m, _ := taskEditor(t, &echoTools{})
	if msg, ok := m.taskCommand("task nope"); !ok || !strings.Contains(msg, "no task named nope") {
		t.Errorf("unknown name: %q", msg)
	}
	if msg, _ := m.taskCommand("task lint"); msg != "" || len(m.terms) != 1 {
		t.Errorf("run by name: msg=%q terms=%d", msg, len(m.terms))
	}
	m2, _ := taskEditor(t, &noTools{})
	msg, _ := m2.taskCommand("task lint")
	if !strings.Contains(msg, "npm is not on your PATH") || len(m2.terms) != 0 {
		t.Errorf("missing tool: %q terms=%d", msg, len(m2.terms))
	}
}

func TestTaskWithNoTaskFilesSaysSo(t *testing.T) {
	m := newEditor(t)
	msg, _ := m.taskCommand("task")
	if !strings.Contains(msg, "no package.json scripts") || m.mode == modeTasks {
		t.Errorf("msg = %q mode=%v", msg, m.mode)
	}
}

// Problems in a finished task's output join :diag and ]d.
func TestTaskOutputProblemsFeedDiagAndJump(t *testing.T) {
	m, alpha := taskEditor(t, &echoTools{})
	m.taskCommand("task build")
	tab := m.terms[0]
	out := "\x1b[31m./app.go:1:3: boom\x1b[0m\r\nsrc/main.go:1: other\r\n"
	tab.task.add([]byte(out))
	m.taskFinished(tab.task, 2)
	if got := m.active().Message(); !strings.Contains(got, "failed (exit 2)") || !strings.Contains(got, "2 problems") {
		t.Errorf("message = %q", got)
	}
	if ds := m.allDiags(filepath.Join(alpha, "app.go")); len(ds) != 1 || ds[0].Line != 0 || ds[0].Col != 2 {
		t.Fatalf("diags = %+v", ds)
	}
	m.termFocus, m.bufFocus = false, true
	feed(m, ":", "d", "i", "a", "g", "enter")
	if m.mode != modeResults || len(m.hits) != 2 {
		t.Fatalf(":diag: mode=%v hits=%d", m.mode, len(m.hits))
	}
	// A later run replaces the earlier task problems.
	tab.task.buf = nil
	m.taskFinished(tab.task, 0)
	if len(m.taskDiags) != 0 {
		t.Errorf("stale task problems kept: %+v", m.taskDiags)
	}
}
