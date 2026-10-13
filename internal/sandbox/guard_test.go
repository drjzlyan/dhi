package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// F-071 hardening: guarded subtrees deny writes inside an rw root, and
// reopened ones stay writable; extending roots keeps the guard.
func TestSeatbeltGuardedProfile(t *testing.T) {
	s, err := NewSeatbelt("/usr/bin/sandbox-exec", []string{"/ws"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.WithGuarded([]string{"/ws/.dhi"}, []string{"/ws/.dhi/worktrees"})
	if err != nil {
		t.Fatal(err)
	}
	p := g.(*Seatbelt).Profile()
	allow := strings.Index(p, `(allow file-write* (subpath "/ws"))`)
	deny := strings.Index(p, `(deny file-write* (subpath "/ws/.dhi"))`)
	reopen := strings.Index(p, `(allow file-write* (subpath "/ws/.dhi/worktrees"))`)
	if allow < 0 || deny < allow || reopen < deny {
		t.Fatalf("guard rules out of order (allow %d, deny %d, reopen %d):\n%s", allow, deny, reopen, p)
	}
	ext, err := g.(*Seatbelt).WithExtraRoots([]string{"/home/u/.claude"})
	if err != nil || !strings.Contains(ext.(*Seatbelt).Profile(), `(deny file-write* (subpath "/ws/.dhi"))`) {
		t.Fatalf("extending roots must keep the guard: %v", err)
	}
}

func TestBubblewrapGuardedBinds(t *testing.T) {
	root := t.TempDir()
	dhi, wt := filepath.Join(root, ".dhi"), filepath.Join(root, ".dhi", "worktrees")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := NewBubblewrap("/usr/bin/bwrap", []string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := b.WithGuarded([]string{dhi, filepath.Join(root, "missing")}, []string{wt})
	args := strings.Join(mustWrap(t, g), " ")
	rw := strings.Index(args, "--bind "+root+" "+root)
	ro := strings.Index(args, "--ro-bind "+dhi+" "+dhi)
	re := strings.Index(args, "--bind "+wt+" "+wt)
	if rw < 0 || ro < rw || re < ro || strings.Contains(args, "missing") {
		t.Fatalf("binds out of order or a missing path bound:\n%s", args)
	}
}

func mustWrap(t *testing.T, s Sandbox) []string {
	t.Helper()
	out, err := s.Wrap([]string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Live on macOS: a guarded control file cannot be written by a wrapped
// child, while the reopened worktree area can.
func TestSeatbeltGuardedLive(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt is macOS-only")
	}
	bin, err := exec.LookPath("sandbox-exec")
	if err != nil {
		t.Skip("sandbox-exec not found")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dhi, wt := filepath.Join(root, ".dhi"), filepath.Join(root, ".dhi", "worktrees")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := NewSeatbelt(bin, []string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.WithGuarded([]string{dhi}, []string{wt})
	if err != nil {
		t.Fatal(err)
	}
	write := func(p string) error {
		argv, err := g.Wrap([]string{"/bin/sh", "-c", "echo x > '" + p + "'"})
		if err != nil {
			return err
		}
		return exec.Command(argv[0], argv[1:]...).Run()
	}
	if err := write(filepath.Join(root, "ok.txt")); err != nil {
		t.Fatalf("the rw root must stay writable: %v", err)
	}
	if err := write(filepath.Join(dhi, "grants.json")); err == nil {
		t.Fatal("a guarded control file was writable")
	}
	if err := write(filepath.Join(wt, "work.txt")); err != nil {
		t.Fatalf("the reopened worktree area must be writable: %v", err)
	}
	// "dir/*": task worktrees are writable, the card files beside them
	// are not (the real .dhi/tasks layout).
	tasks := filepath.Join(dhi, "tasks")
	if err := os.MkdirAll(filepath.Join(tasks, "fix.split", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	g2, err := s.WithGuarded([]string{dhi}, []string{tasks + "/*"})
	if err != nil {
		t.Fatal(err)
	}
	g = g2
	if err := write(filepath.Join(tasks, "fix.split.toml")); err == nil {
		t.Fatal("a task card beside the worktrees was writable")
	}
	if err := write(filepath.Join(tasks, "fix.split", "app", "money.go")); err != nil {
		t.Fatalf("a task worktree must be writable: %v", err)
	}
}

// "dir/*" binds each existing subdirectory writable, never the files
// beside them, expanded at wrap time.
func TestBubblewrapReopenEachSubdir(t *testing.T) {
	root := t.TempDir()
	dhi := filepath.Join(root, ".dhi")
	wt := filepath.Join(dhi, "tasks", "fix", "app")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dhi, "tasks", "fix.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := NewBubblewrap("/usr/bin/bwrap", []string{root}, nil)
	g, _ := b.WithGuarded([]string{dhi}, []string{filepath.Join(dhi, "tasks") + "/*"})
	args := strings.Join(mustWrap(t, g), " ")
	fix := filepath.Join(dhi, "tasks", "fix")
	if !strings.Contains(args, "--bind "+fix+" "+fix) || strings.Contains(args, "fix.toml") {
		t.Fatalf("each-subdir reopen:\n%s", args)
	}
}
