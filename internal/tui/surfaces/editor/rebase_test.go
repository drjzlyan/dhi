package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rebaseEnv is a git environment that ignores the developer's own config.
func rebaseEnv() []string {
	return append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@example.com", "GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@example.com")
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = dir, rebaseEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitFile writes name and commits it with subject.
func commitFile(t *testing.T, dir, name, content, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", name)
	gitT(t, dir, "commit", "-q", "-m", subject)
}

func subjects(t *testing.T, dir string) string {
	return strings.ReplaceAll(gitT(t, dir, "log", "--reverse", "--format=%s"), "\n", ",")
}

// rebaseEditor is the alpha repo with the commits initial, c2, c3, c4 on
// master, git attached, and app.go open.
func rebaseEditor(t *testing.T) (*Model, string) {
	t.Helper()
	m, alpha := gitDepthEditor(t)
	m.termEnv = rebaseEnv
	commitFile(t, alpha, "f2.txt", "two\n", "c2")
	commitFile(t, alpha, "f3.txt", "three\n", "c3")
	commitFile(t, alpha, "f4.txt", "four\n", "c4")
	m.open(&node{kind: nodeFile, path: filepath.Join(alpha, "app.go"), name: "app.go"})
	t.Cleanup(m.closeRebaseServer)
	return m, alpha
}

// pump runs the UI loop's drain until cond holds.
func pump(t *testing.T, m *Model, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		m.drainTerm()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func waitEdit(t *testing.T, m *Model, base string) {
	t.Helper()
	pump(t, m, "git to ask for "+base, func() bool {
		if tab := m.activeBufTab(); tab != nil && tab.edit != nil && filepath.Base(tab.path) == base {
			return true
		}
		return false
	})
}

func (m *Model) activeBufTab() *bufTab {
	if m.activeTab < len(m.bufs) {
		return m.bufs[m.activeTab]
	}
	return nil
}

func rebaseTabDone(m *Model) bool {
	for _, tb := range m.terms {
		if tb.rebase && !tb.exited {
			return false
		}
	}
	return true
}

// F-165: the whole path with the user's git — :rebase opens the todo in a
// buffer, a vim-style reorder plus :wq is what git rebases with.
func TestRebaseReorderThroughTheBuffer(t *testing.T) {
	m, alpha := rebaseEditor(t)
	base := gitT(t, alpha, "rev-list", "--max-parents=0", "HEAD")
	e := m.active()
	typeKeys(m, ":rebase "+base)
	feed(m, "enter")
	waitEdit(t, m, "git-rebase-todo")

	tab := m.activeBufTab()
	text := tab.ed.Buffer().Text()
	if !strings.HasPrefix(text, "pick ") || !strings.Contains(text, " c2\n") {
		t.Fatalf("todo:\n%s", text)
	}
	if m.termFocus || !m.bufFocus {
		t.Fatal("the todo buffer should have focus")
	}
	if !strings.Contains(tab.ed.Message(), "reorder") {
		t.Errorf("hint = %q", tab.ed.Message())
	}
	feed(m, "g", "g", "d", "d", "p") // c2 below c3
	typeKeys(m, ":wq")
	feed(m, "enter")
	pump(t, m, "the rebase to finish", func() bool { return rebaseTabDone(m) && m.rebase == nil })

	if got := subjects(t, alpha); got != "initial,c3,c2,c4" {
		t.Fatalf("history = %s", got)
	}
	if rebaseInProgress(m, alpha) {
		t.Fatal("rebase still in progress")
	}
	_ = e
}

// F-165: :cq leaves the todo, git aborts, history is untouched.
func TestRebaseCQAbortsTheStep(t *testing.T) {
	m, alpha := rebaseEditor(t)
	base := gitT(t, alpha, "rev-list", "--max-parents=0", "HEAD")
	typeKeys(m, ":rebase "+base)
	feed(m, "enter")
	waitEdit(t, m, "git-rebase-todo")
	feed(m, "d", "d")
	typeKeys(m, ":cq")
	feed(m, "enter")
	pump(t, m, "git to give up", func() bool { return rebaseTabDone(m) && m.rebase == nil })
	if got := subjects(t, alpha); got != "initial,c2,c3,c4" {
		t.Fatalf("history = %s", got)
	}
	if rebaseInProgress(m, alpha) {
		t.Fatal("an aborted todo must not leave a rebase running")
	}
}

// F-165 + F-156: a reorder that conflicts stops the rebase; :conflict resolves,
// :rebase continue opens the commit message in a buffer and finishes.
func TestRebaseConflictThenContinue(t *testing.T) {
	m, alpha := rebaseEditor(t)
	commitFile(t, alpha, "shared.txt", "base\n", "c5")
	commitFile(t, alpha, "shared.txt", "one\n", "c6")
	commitFile(t, alpha, "shared.txt", "two\n", "c7")
	base := gitT(t, alpha, "rev-parse", "HEAD~2")
	typeKeys(m, ":rebase "+base)
	feed(m, "enter")
	waitEdit(t, m, "git-rebase-todo")
	feed(m, "g", "g", "d", "d", "p") // c7 before c6: they conflict
	typeKeys(m, ":wq")
	feed(m, "enter")
	pump(t, m, "the rebase to stop", func() bool { return rebaseTabDone(m) })

	if !rebaseInProgress(m, alpha) || m.rebase == nil {
		t.Fatalf("expected a stopped rebase, history = %s", subjects(t, alpha))
	}
	if m.toast == nil || m.toast.Title != "Rebase stopped" || !strings.Contains(m.toast.Detail, ":conflict") {
		t.Fatalf("toast = %+v", m.toast)
	}
	m.open(&node{kind: nodeFile, path: filepath.Join(alpha, "shared.txt"), name: "shared.txt"})
	e := m.active()
	if !strings.Contains(e.Buffer().Text(), "<<<<<<<") {
		t.Fatalf("no conflict markers:\n%s", e.Buffer().Text())
	}
	typeKeys(m, ":conflict theirs")
	feed(m, "enter")
	typeKeys(m, ":w")
	feed(m, "enter")
	if !strings.Contains(e.Message(), "stage shared.txt") {
		t.Fatalf("no stage offer: %q", e.Message())
	}
	feed(m, "y") // F-156: stage the resolved file
	if u := gitT(t, alpha, "diff", "--name-only", "--diff-filter=U"); u != "" {
		t.Fatalf("still unmerged: %s", u)
	}

	typeKeys(m, ":rebase continue")
	feed(m, "enter")
	// The commit message may open as a buffer; answer it like a user.
	again := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && (!rebaseTabDone(m) || m.rebase != nil) {
		m.drainTerm()
		if rebaseTabDone(m) && rebaseInProgress(m, alpha) && !again {
			// c6 on top of c7 conflicts too; settle it with the CLI
			again = true
			_ = os.WriteFile(filepath.Join(alpha, "shared.txt"), []byte("final\n"), 0o644)
			gitT(t, alpha, "add", "shared.txt")
			typeKeys(m, ":rebase continue")
			feed(m, "enter")
		}
		if tab := m.activeBufTab(); tab != nil && tab.edit != nil {
			if filepath.Base(tab.path) != "COMMIT_EDITMSG" {
				t.Fatalf("unexpected edit of %s", tab.path)
			}
			typeKeys(m, ":wq")
			feed(m, "enter")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rebaseInProgress(m, alpha) {
		t.Fatalf("rebase did not finish; status:\n%s", gitT(t, alpha, "status"))
	}
	if got := subjects(t, alpha); !strings.HasPrefix(got, "initial,c2,c3,c4,c5,") || strings.Count(got, ",") != 6 {
		t.Fatalf("history = %s", got)
	}
}

// :rebase abort runs git's abort and the shim goes away.
func TestRebaseAbort(t *testing.T) {
	m, alpha := rebaseEditor(t)
	commitFile(t, alpha, "shared.txt", "base\n", "c5")
	commitFile(t, alpha, "shared.txt", "one\n", "c6")
	commitFile(t, alpha, "shared.txt", "two\n", "c7")
	base := gitT(t, alpha, "rev-parse", "HEAD~2")
	typeKeys(m, ":rebase "+base)
	feed(m, "enter")
	waitEdit(t, m, "git-rebase-todo")
	feed(m, "g", "g", "d", "d", "p")
	typeKeys(m, ":wq")
	feed(m, "enter")
	pump(t, m, "the rebase to stop", func() bool { return rebaseTabDone(m) && rebaseInProgress(m, alpha) })
	m.open(&node{kind: nodeFile, path: filepath.Join(alpha, "app.go"), name: "app.go"})
	before := subjects(t, alpha)
	_ = before
	typeKeys(m, ":rebase abort")
	feed(m, "enter")
	pump(t, m, "the abort", func() bool { return rebaseTabDone(m) && m.rebase == nil })
	if rebaseInProgress(m, alpha) {
		t.Fatal("abort left the rebase running")
	}
	if got := subjects(t, alpha); got != "initial,c2,c3,c4,c5,c6,c7" {
		t.Fatalf("history = %s", got)
	}
}

func TestRebaseWithoutOneInProgress(t *testing.T) {
	m, _ := rebaseEditor(t)
	e := m.active()
	for _, sub := range []string{"continue", "abort", "skip"} {
		msg, ok := m.rebaseCommand(e, "rebase "+sub)
		if !ok || !strings.Contains(msg, "none in progress") {
			t.Errorf("%s: %q", sub, msg)
		}
	}
	if _, ok := m.rebaseCommand(e, "rebased"); ok {
		t.Error("rebased is not :rebase")
	}
	if msg, _ := m.rebaseCommand(e, "rebase a b"); msg != rebaseUsage {
		t.Errorf("usage: %q", msg)
	}
}

func TestRebaseBaseChoice(t *testing.T) {
	m, alpha := rebaseEditor(t)
	// master only, no upstream: the merge-base with the default branch is
	// HEAD itself, so there is nothing to rebase and it says so.
	if _, msg := m.rebaseBase(alpha); !strings.Contains(msg, "no commits beyond master") {
		t.Fatalf("msg = %q", msg)
	}
	// A feature branch: the base is where it left master.
	fork := gitT(t, alpha, "rev-parse", "HEAD")
	gitT(t, alpha, "checkout", "-q", "-b", "feature")
	commitFile(t, alpha, "g.txt", "g\n", "g1")
	if base, msg := m.rebaseBase(alpha); msg != "" || base != fork {
		t.Fatalf("base = %q msg = %q (want %s)", base, msg, fork)
	}
	// Both main and master exist: ask.
	gitT(t, alpha, "branch", "main", fork)
	if _, msg := m.rebaseBase(alpha); !strings.Contains(msg, "ambiguous") || !strings.Contains(msg, "main, master") {
		t.Fatalf("msg = %q", msg)
	}
	// An upstream wins without asking.
	gitT(t, alpha, "branch", "--set-upstream-to=main")
	if base, msg := m.rebaseBase(alpha); msg != "" || base != "main" {
		t.Fatalf("base = %q msg = %q", base, msg)
	}
	// No default branch at all: ask.
	gitT(t, alpha, "branch", "--unset-upstream")
	gitT(t, alpha, "branch", "-D", "main")
	gitT(t, alpha, "branch", "-m", "master", "trunk2")
	if _, msg := m.rebaseBase(alpha); !strings.Contains(msg, "no upstream and no default branch") {
		t.Fatalf("msg = %q", msg)
	}
}

// :q on the untouched todo is a plain close: the program carries on with it.
func TestRebaseQuitWithoutSavingKeepsTheTodo(t *testing.T) {
	m, alpha := rebaseEditor(t)
	_ = alpha
	typeKeys(m, ":rebase HEAD~1")
	feed(m, "enter")
	waitEdit(t, m, "git-rebase-todo")
	if m.rebase == nil || m.activeBufTab().edit == nil {
		t.Fatal("expected a waiting edit")
	}
	typeKeys(m, ":q")
	feed(m, "enter")
	pump(t, m, "git to continue", func() bool { return rebaseTabDone(m) && m.rebase == nil })
}
