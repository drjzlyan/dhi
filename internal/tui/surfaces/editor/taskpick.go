package editor

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/taskrun"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// Task runner (F-146). `:task` lists what the project's own task files
// declare (package.json scripts, Makefile targets, justfile recipes) per
// member; enter runs one in a drawer tab in that folder with the user's
// tool. When the tab exits its output is scanned for file:line:col
// problems, which join the :diag list.

// taskCapMax bounds the output kept for the problem scan.
const taskCapMax = 1 << 20

// taskCapture is a task tab's output, kept for the problem scan.
type taskCapture struct {
	name string
	dir  string
	buf  []byte
}

func (c *taskCapture) add(chunk []byte) {
	c.buf = append(c.buf, chunk...)
	if len(c.buf) > taskCapMax {
		c.buf = append([]byte(nil), c.buf[len(c.buf)-taskCapMax:]...)
	}
}

// allDiags is the language servers' problems for path plus those found in
// task output, in no particular order.
func (m *Model) allDiags(path string) []lsp.Diagnostic {
	out := append([]lsp.Diagnostic(nil), m.lspDiags[path]...)
	return append(out, m.taskDiags[path]...)
}

// hostJustList runs the user's `just --list --unsorted`; nil without just.
func (m *Model) hostJustList() taskrun.JustList {
	if m.tools == nil {
		return nil
	}
	bin, ok := m.tools.Find("just")
	if !ok {
		return nil
	}
	env := m.env()
	return func(dir string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "--list", "--unsorted")
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.Output()
		return string(out), err
	}
}

// taskDirs are the folders searched: each workspace member.
func (m *Model) taskDirs() []memberRef {
	if len(m.members) > 0 {
		return m.members
	}
	if m.ws != nil && m.ws.Root != "" {
		return []memberRef{{name: "workspace", path: m.ws.Root}}
	}
	return nil
}

// discoverTasks lists every member's tasks; notes name unreadable files.
func (m *Model) discoverTasks() (items []kit.PaletteItem, notes []string) {
	dirs := m.taskDirs()
	just := m.hostJustList()
	for _, d := range dirs {
		ts, ns := taskrun.Discover(d.path, just)
		for _, n := range ns {
			notes = append(notes, d.name+": "+n)
		}
		for _, t := range ts {
			group := ""
			if len(dirs) > 1 {
				group = d.name
			}
			items = append(items, kit.PaletteItem{Group: group, Title: t.Name, Hint: t.Runner + " · " + oneLine(t.Hint), Data: t})
		}
	}
	return items, notes
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}

// taskCommand handles :task [name]. Bare, it opens the picker; with a
// name, it runs the one task of that exact name.
func (m *Model) taskCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || (f[0] != "task" && f[0] != "tasks") {
		return "", false
	}
	items, notes := m.discoverTasks()
	if len(items) == 0 {
		msg := "task: no package.json scripts, Makefile targets or justfile recipes here"
		if len(notes) > 0 {
			msg = "task: " + strings.Join(notes, " · ")
		}
		return msg, true
	}
	if len(f) > 1 {
		var hit []taskrun.Task
		for _, it := range items {
			if t := it.Data.(taskrun.Task); t.Name == f[1] {
				hit = append(hit, t)
			}
		}
		switch len(hit) {
		case 0:
			return "task: no task named " + f[1], true
		case 1:
			return m.runTask(hit[0]), true
		}
		return "task: " + f[1] + " is in several places — use :task and pick one", true
	}
	m.taskPick = kit.NewPalette(items)
	m.taskPick.Title, m.taskPick.Noun = "run a task", "tasks"
	m.mode = modeTasks
	if len(notes) > 0 {
		return strings.Join(notes, " · "), true
	}
	return "", true
}

func (m *Model) handleTaskKey(key string) bool {
	p := m.taskPick
	if p == nil {
		m.mode = modeNav
		return false
	}
	picked, closed := p.HandleKey(key)
	if closed {
		m.mode, m.taskPick = modeNav, nil
	}
	if picked != nil {
		m.note(m.runTask(picked.Data.(taskrun.Task)))
	}
	return true
}

func (m *Model) taskPickView() string {
	return kit.Center(m.taskPick.View(min(maxInt(m.width-8, 40), 76)), m.width, m.height)
}

// runTask opens a focused drawer tab running t with the user's tool.
// It returns a message when the tool is missing.
func (m *Model) runTask(t taskrun.Task) string {
	argv := append([]string(nil), t.Argv...)
	if m.tools != nil {
		bin, ok := m.tools.Find(argv[0])
		if !ok {
			return "task: " + argv[0] + " is not on your PATH"
		}
		argv[0] = bin
	}
	m.runInTab(t.Runner+" "+t.Name, t.Dir, argv)
	m.terms[len(m.terms)-1].task = &taskCapture{name: t.Name, dir: t.Dir}
	return ""
}

// taskFinished scans a task tab's output for problems and replaces the
// previous task problems with them.
func (m *Model) taskFinished(c *taskCapture, code int) {
	probs := taskrun.ParseProblems(ansi.Strip(string(c.buf)), c.dir)
	m.taskDiags = map[string][]lsp.Diagnostic{}
	for _, p := range probs {
		m.taskDiags[p.Path] = append(m.taskDiags[p.Path], lsp.Diagnostic{
			Path: p.Path, Line: p.Line - 1, Col: max(p.Col-1, 0), Severity: p.Severity, Message: p.Message})
	}
	for _, ds := range m.taskDiags {
		sort.SliceStable(ds, func(i, j int) bool { return ds[i].Line < ds[j].Line })
	}
	verdict := "finished"
	if code > 0 {
		verdict = fmt.Sprintf("failed (exit %d)", code)
	}
	msg := "task " + c.name + " " + verdict
	if len(probs) > 0 {
		msg += " — " + kit.Count(len(probs), "problem") + ", :diag lists them"
	}
	m.note(msg)
}
