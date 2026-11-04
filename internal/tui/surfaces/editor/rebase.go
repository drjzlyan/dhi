package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/editshim"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// Interactive rebase (F-165): `git rebase -i` runs in a terminal tab like
// push and pull (F-121), but its todo list and commit messages open in DHI's
// own buffers instead of a nested vim. git is started with GIT_SEQUENCE_EDITOR
// and GIT_EDITOR naming the editshim link; each file git wants edited arrives
// here, opens as a buffer, and git waits until that buffer closes (:wq, :q) or
// is abandoned (:cq, which makes git abort the step).

// rebaseState is the shim server for the rebase in progress.
type rebaseState struct {
	srv *editshim.Server
	dir string
}

// editJob ties a buffer to the program waiting for it.
type editJob struct{ req editshim.Request }

const rebaseUsage = "usage: :rebase [<base>] | :rebase continue | abort | skip"

// rebaseCommand handles :rebase.
func (m *Model) rebaseCommand(requester interface{ Path() string }, cmd string) (string, bool) {
	cmd = strings.TrimSpace(cmd)
	arg, ok := strings.CutPrefix(cmd, "rebase")
	if !ok || (arg != "" && arg[0] != ' ') {
		return "", false
	}
	arg = strings.TrimSpace(arg)
	if m.gitCLI == nil {
		return "rebase: git is not available", true
	}
	if m.termMsgs == nil {
		return "rebase: no terminal here", true
	}
	dir := m.rebaseDir(requester.Path())
	if dir == "" {
		return "rebase: open a file in a repository first", true
	}
	switch arg {
	case "continue", "abort", "skip":
		if !rebaseInProgress(m, dir) {
			return "rebase: none in progress in " + filepath.Base(dir), true
		}
		return m.startRebaseTab(dir, "--"+arg)
	}
	if strings.ContainsAny(arg, " \t") {
		return rebaseUsage, true
	}
	if rebaseInProgress(m, dir) {
		return "rebase: already in progress — :rebase continue | abort | skip", true
	}
	base, msg := arg, ""
	if base == "" {
		if base, msg = m.rebaseBase(dir); msg != "" {
			return msg, true
		}
	}
	return m.startRebaseTab(dir, "-i", base)
}

// rebaseDir is the repository the rebase acts on: the one running it, the
// buffer's member, else the git panel's.
func (m *Model) rebaseDir(path string) string {
	if m.rebase != nil && m.rebase.dir != "" {
		return m.rebase.dir
	}
	if d := m.memberRootOf(path); d != "" {
		return d
	}
	return m.currentGitRoot()
}

// rebaseInProgress reports a rebase stopped (or running) in dir.
func rebaseInProgress(m *Model, dir string) bool {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		out, err := m.gitRun(dir, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		p := strings.TrimSpace(out)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// rebaseBase picks what to rebase onto: the upstream, else the merge-base with
// the default branch. When neither is clear it says so and asks (ADR-0011).
func (m *Model) rebaseBase(dir string) (string, string) {
	if up, err := m.gitRun(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		if up = strings.TrimSpace(up); up != "" {
			return up, ""
		}
	}
	var cands []string
	if def, err := m.gitRun(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && strings.TrimSpace(def) != "" {
		cands = []string{strings.TrimSpace(def)}
	} else {
		for _, b := range []string{"main", "master", "trunk"} {
			if _, err := m.gitRun(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err == nil {
				cands = append(cands, b)
			}
		}
	}
	switch len(cands) {
	case 0:
		return "", "rebase: no upstream and no default branch found — :rebase <base>"
	case 1:
	default:
		return "", "rebase: ambiguous base (" + strings.Join(cands, ", ") + ") — :rebase <base>"
	}
	mb, err := m.gitRun(dir, "merge-base", "HEAD", cands[0])
	if err != nil {
		return "", "rebase: no common ancestor with " + cands[0] + " — :rebase <base>"
	}
	mb, head := strings.TrimSpace(mb), ""
	if h, err := m.gitRun(dir, "rev-parse", "HEAD"); err == nil {
		head = strings.TrimSpace(h)
	}
	if mb == head {
		return "", "rebase: no commits beyond " + cands[0] + " — :rebase <base> to pick another"
	}
	return mb, ""
}

// startRebaseTab runs `git rebase <args>` in a tab whose editors are the shim.
func (m *Model) startRebaseTab(dir string, args ...string) (string, bool) {
	srv, err := m.rebaseServer(dir)
	if err != nil {
		return "rebase: " + err.Error(), true
	}
	argv := append([]string{"git", "rebase"}, args...)
	t := m.runInTabEnv("rebase", dir, argv, srv.Env())
	t.rebase = true
	m.gitFocus = false
	return "git " + strings.Join(argv[1:], " ") + " — the todo list opens here; :wq continues, :cq aborts", true
}

// rebaseServer returns the running shim server, starting one if needed.
func (m *Model) rebaseServer(dir string) (*editshim.Server, error) {
	if m.rebase != nil {
		m.rebase.dir = dir
		return m.rebase.srv, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	srv, err := editshim.Start(exe)
	if err != nil {
		return nil, err
	}
	m.rebase = &rebaseState{srv: srv, dir: dir}
	ch := m.termMsgs
	go func() {
		for {
			select {
			case req := <-srv.Requests():
				select {
				case ch <- teaMsg{kind: editMsgOpen, editReq: &req}:
				case <-srv.Closed():
					req.Done(false)
					return
				}
			case <-srv.Closed():
				return
			}
		}
	}()
	return srv, nil
}

// closeRebaseServer stops the shim; a program still waiting on it fails.
func (m *Model) closeRebaseServer() {
	if m.rebase != nil {
		m.rebase.srv.Close()
		m.rebase = nil
	}
}

// rebaseStepDone runs when a rebase tab's git exits. With no rebase left in
// the repository the shim goes away; a rebase stopped on a conflict says how
// to go on.
func (m *Model) rebaseStepDone(dir string) {
	m.invalidateGutters()
	m.refreshGitData()
	if !rebaseInProgress(m, dir) {
		m.closeRebaseServer()
		return
	}
	detail := ":rebase continue, abort or skip"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if list, err := gitcore.Conflicts(ctx, m.gitCLI, dir); err == nil && len(list) > 0 {
		detail = "resolve the conflicts (:conflict ours|theirs|both), stage, then :rebase continue"
	}
	m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Rebase stopped", Detail: detail}
}

// applyEditRequest opens the file git asked for and remembers who waits.
func (m *Model) applyEditRequest(req *editshim.Request) {
	if req == nil {
		return
	}
	if t := m.editJobTab(req.Path); t != nil { // an earlier, abandoned wait on the same file
		t.edit.req.Done(false)
		t.edit = nil
	}
	for i, t := range m.bufs {
		if t.path == req.Path {
			m.activeTab = i
			m.closeBuffer()
			break
		}
	}
	m.open(&node{kind: nodeFile, path: req.Path, name: filepath.Base(req.Path)})
	if t := m.bufTabFor(req.Path); t != nil {
		t.edit = &editJob{req: *req}
		m.termFocus = false
		hint := "commit message — :wq saves and goes on · :cq aborts"
		if filepath.Base(req.Path) == "git-rebase-todo" {
			hint = "rebase todo: pick reword edit squash fixup drop — reorder lines · :wq starts · :cq aborts"
		}
		t.ed.SetMessage(hint)
		return
	}
	req.Done(false) // it could not be opened: git must not wait forever
}

// bufTabFor is the open tab holding path.
func (m *Model) bufTabFor(path string) *bufTab {
	for _, t := range m.bufs {
		if t.path == path {
			return t
		}
	}
	return nil
}

// editJobTab is the tab a program is waiting on at path, if any.
func (m *Model) editJobTab(path string) *bufTab {
	if t := m.bufTabFor(path); t != nil && t.edit != nil {
		return t
	}
	return nil
}

// finishEdit answers the program waiting on a closing buffer: success unless
// the buffer was left with :cq.
func (m *Model) finishEdit(t *bufTab) {
	if t.edit == nil {
		return
	}
	t.edit.req.Done(!t.ed.Aborted())
	t.edit = nil
}
