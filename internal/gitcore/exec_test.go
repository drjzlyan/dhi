package gitcore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGit installs an executable stub at dir/git that appends its
// arguments to $FAKE_LOG and reacts to a few FAKE_* controls. It lets
// tests assert exact argument construction and output parsing without
// a real git binary.
func fakeGit(t *testing.T, extra string) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "git")
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> \"$FAKE_LOG\"\n" +
		extra + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls.log")
	env := []string{
		"FAKE_LOG=" + log,
		"PATH=" + os.Getenv("PATH"), // sh needs nothing, but keep sane
	}
	return NewRunner(bin, env), log
}

func readLog(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

const porcelainFixture = `worktree /repo/main
HEAD 1111111111111111111111111111111111111111
branch refs/heads/main

worktree /repo/wt-detached
HEAD 2222222222222222222222222222222222222222
detached

worktree /repo/wt-task
HEAD 3333333333333333333333333333333333333333
branch refs/heads/task/x
bare

`

func TestVersionParses(t *testing.T) {
	r, _ := fakeGit(t, `if [ "$1" = "--version" ]; then echo "git version 2.55.0"; fi`)
	v, err := r.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "2.55.0" {
		t.Errorf("Version=%q, want 2.55.0", v)
	}
}

func TestRunReportsStderrOnFailure(t *testing.T) {
	r, log := fakeGit(t, `if [ "$1" = "status" ]; then echo "fatal: not a git repository" >&2; exit 3; fi`)
	_, _, err := r.Run(context.Background(), "", "status", "--porcelain")
	if err == nil {
		t.Fatal("expected error from failing stub")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error lacks stderr detail: %v", err)
	}
	if lines := readLog(t, log); len(lines) != 1 || lines[0] != "status --porcelain" {
		t.Errorf("logged args = %v", lines)
	}
}

func TestWorktreeAddArgumentOrder(t *testing.T) {
	r, log := fakeGit(t, "")
	repo := t.TempDir()
	if err := r.WorktreeAdd(context.Background(), repo, "/wt/path", "task/x", "main"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	want := "worktree add -b task/x /wt/path main"
	lines := readLog(t, log)
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("args = %v, want [%s]", lines, want)
	}
}

func TestWorktreeAddOmitsEmptyStartpoint(t *testing.T) {
	r, log := fakeGit(t, "")
	repo := t.TempDir()
	if err := r.WorktreeAdd(context.Background(), repo, "/wt/p", "b", ""); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	if got := readLog(t, log)[0]; got != "worktree add -b b /wt/p" {
		t.Errorf("args = %q", got)
	}
}

func TestWorktreeRemoveForceFlag(t *testing.T) {
	r, log := fakeGit(t, "")
	repo := t.TempDir()
	if err := r.WorktreeRemove(context.Background(), repo, "/wt/p", false); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if got := readLog(t, log)[0]; got != "worktree remove /wt/p" {
		t.Errorf("safe remove args = %q", got)
	}
	if err := r.WorktreeRemove(context.Background(), repo, "/wt/p", true); err != nil {
		t.Fatalf("WorktreeRemove force: %v", err)
	}
	if got := readLog(t, log)[1]; got != "worktree remove --force /wt/p" {
		t.Errorf("force remove args = %q", got)
	}
}

func TestPruneArgs(t *testing.T) {
	r, log := fakeGit(t, "")
	repo := t.TempDir()
	if err := r.Prune(context.Background(), repo); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if got := readLog(t, log); len(got) != 1 || got[0] != "worktree prune" {
		t.Errorf("prune args = %v", got)
	}
}

func TestWorktreeListParsesPorcelainAndSorts(t *testing.T) {
	r, _ := fakeGit(t, `if [ "$2" = "list" ]; then printf '%s' "$FAKE_PORCELAIN"; fi`)
	r.env = append(r.env, "FAKE_PORCELAIN="+porcelainFixture)
	wts, err := r.WorktreeList(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(wts) != 3 {
		t.Fatalf("got %d worktrees, want 3: %+v", len(wts), wts)
	}
	// Sorted by path despite fixture order.
	if wts[0].Path != "/repo/main" || wts[1].Path != "/repo/wt-detached" || wts[2].Path != "/repo/wt-task" {
		t.Errorf("order wrong: %v", wts)
	}
	main := wts[0]
	if main.Branch != "main" || main.Detached || main.Bare || main.Head[0] != '1' {
		t.Errorf("main entry wrong: %+v", main)
	}
	det := wts[1]
	if !det.Detached || det.Branch != "" {
		t.Errorf("detached entry wrong: %+v", det)
	}
	task := wts[2]
	if task.Branch != "task/x" || !task.Bare || task.Detached {
		t.Errorf("task entry wrong: %+v", task)
	}
}

func TestTimeoutKillsHangingProcess(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "git")
	script := "#!/bin/sh\nsleep 30\necho late\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(bin, nil)
	r.timeout = 150 * time.Millisecond
	start := time.Now()
	if _, _, err := r.Run(context.Background(), "", "--version"); err == nil {
		t.Fatal("expected timeout error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("timeout took %v, process not killed promptly", d)
	}
}

func TestParseWorktreePorcelainEmptyInput(t *testing.T) {
	if wts := parseWorktreePorcelain(""); len(wts) != 0 {
		t.Errorf("empty input yielded %+v", wts)
	}
	if wts := parseWorktreePorcelain("\n\n"); len(wts) != 0 {
		t.Errorf("blank input yielded %+v", wts)
	}
}
