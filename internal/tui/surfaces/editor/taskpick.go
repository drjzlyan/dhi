package editor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// taskSource is one folder to list tasks in, with the `just --list`
// runner it may use (nil: read the justfile only).
type taskSource struct {
	dir  memberRef
	just taskrun.JustList
}

// taskSources decides, on the UI goroutine, where to look and whether
// `just` may run there.
func (m *Model) taskSources() []taskSource {
	hostJust := m.hostJustList()
	var out []taskSource
	for _, d := range m.taskDirs() {
		// F-148: `just --list` evaluates the justfile (backticks, imports),
		// so a folder that is not trusted gets its recipes by reading the
		// file only; nothing of the repo's runs to list tasks.
		just := hostJust
		if m.trust != nil && !m.trust.Trusted(m.trustRoot(d.path)) {
			just = nil
		}
		out = append(out, taskSource{dir: d, just: just})
	}
	return out
}

// runsJust reports a source list that will start `just` (a process the
// UI must not wait for): just is installed, trusted, and a justfile is
// there.
func runsJust(srcs []taskSource) bool {
	for _, s := range srcs {
		if s.just == nil {
			continue
		}
		for _, name := range []string{"justfile", "Justfile", ".justfile"} {
			if _, err := os.Stat(filepath.Join(s.dir.path, name)); err == nil {
				return true
			}
		}
	}
	return false
}

// findTasks lists every source's tasks; notes name unreadable files.
func findTasks(srcs []taskSource) (items []kit.PaletteItem, notes []string) {
	for _, src := range srcs {
		d := src.dir
		ts, ns := taskrun.Discover(d.path, src.just)
		for _, n := range ns {
			notes = append(notes, d.name+": "+n)
		}
		for _, t := range ts {
			group := ""
			if len(srcs) > 1 {
				group = d.name
			}
			items = append(items, kit.PaletteItem{Group: group, Title: t.Name, Hint: t.Runner + " · " + oneLine(t.Hint), Data: t})
		}
	}
	return items, notes
}

// discoverTasks lists every member's tasks; notes name unreadable files.
func (m *Model) discoverTasks() (items []kit.PaletteItem, notes []string) {
	return findTasks(m.taskSources())
}

// tasksFound is a background task listing for :task args.
type tasksFound struct {
	args  []string
	items []kit.PaletteItem
	notes []string
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
	srcs := m.taskSources()
	if runsJust(srcs) {
		// `just --list` evaluates the justfile: off the UI goroutine.
		if m.tasksLoading {
			return "task: still looking for tasks…", true
		}
		m.tasksLoading = true
		ch := m.termMsgs
		go func() {
			items, notes := findTasks(srcs)
			ch <- teaMsg{kind: taskMsgFound, tasks: &tasksFound{args: f, items: items, notes: notes}}
		}()
		return "task: looking for tasks…", true
	}
	items, notes := findTasks(srcs)
	return m.showTasks(f, items, notes), true
}

// applyTasksFound continues :task with a background listing.
func (m *Model) applyTasksFound(r *tasksFound) {
	m.tasksLoading = false
	if r == nil {
		return
	}
	msg := m.showTasks(r.args, r.items, r.notes)
	if msg == "" {
		return
	}
	if e := m.active(); e != nil {
		e.SetMessage(msg)
	} else {
		m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Tasks", Detail: msg}
	}
}

// showTasks runs the named task or opens the picker over items.
func (m *Model) showTasks(f []string, items []kit.PaletteItem, notes []string) string {
	if len(items) == 0 {
		msg := "task: no package.json scripts, Makefile targets or justfile recipes here"
		if len(notes) > 0 {
			msg = "task: " + strings.Join(notes, " · ")
		}
		return msg
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
			return "task: no task named " + f[1]
		case 1:
			return m.runTask(hit[0])
		}
		return "task: " + f[1] + " is in several places — use :task and pick one"
	}
	m.taskPick = kit.NewPalette(items)
	m.taskPick.Title, m.taskPick.Noun = "run a task", "tasks"
	m.mode = modeTasks
	if len(notes) > 0 {
		return strings.Join(notes, " · ")
	}
	return ""
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
	if m.restricted(t.Dir, "run "+t.Runner+" tasks") {
		return trustLine("run " + t.Runner + " tasks")
	}
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
