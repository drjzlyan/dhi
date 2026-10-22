package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// rootsSandbox records the roots a spawn's sandbox was grown by.
type rootsSandbox struct {
	sandbox.Noop
	extra, ro []string
}

func (s rootsSandbox) WithExtraRoots(rw []string) (sandbox.Sandbox, error) {
	return rootsSandbox{extra: append(append([]string(nil), s.extra...), rw...), ro: s.ro}, nil
}

func (s rootsSandbox) WithExtraReadOnly(ro []string) (sandbox.Sandbox, error) {
	return rootsSandbox{extra: s.extra, ro: append(append([]string(nil), s.ro...), ro...)}, nil
}

func firstPending(t *testing.T, a *tools.Approvals) *tools.Approval {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if l := a.List(); len(l) > 0 {
			return l[0]
		}
		select {
		case <-a.Changes():
		case <-deadline:
			t.Fatal("no approval card")
		}
	}
}

// F-087: what may never be granted is refused by name before any card.
func TestAccessTargetRefusesGuardedAndBroadPaths(t *testing.T) {
	h := episodeHarness(t)
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		"/", "~", "relative/dir",
		filepath.Join(h.ws.Root, ".dhi"), filepath.Join(h.ws.Root, ".dhi", "agents"),
		filepath.Join(home, ".ssh", "keys"),
	} {
		if _, err := h.rt.accessTarget(p); err == nil {
			t.Errorf("accessTarget(%q) granted", p)
		}
	}
	got, err := h.rt.accessTarget(filepath.Join(t.TempDir(), "out", "new"))
	if err != nil || !filepath.IsAbs(got) {
		t.Fatalf("plain folder refused: %q %v", got, err)
	}
}

// permission_request for a folder parks a card; a yes for the workspace
// grows the next spawn's sandbox and asks the turn to continue.
func TestPermissionRequestGrantsWriteAtTheNextSpawn(t *testing.T) {
	h := episodeHarness(t)
	_ = mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "export the data"})
	rl := &relaunch{}
	ask := h.rt.accessSeam("scout", rl)
	target := realPath(t.TempDir())
	done := make(chan string, 1)
	go func() {
		out, err := ask(context.Background(), "write", target, "the export goes there")
		if err != nil {
			out = "ERR " + err.Error()
		}
		done <- out
	}()
	ap := firstPending(t, h.approvals)
	if !ap.IsAccess() || ap.Target != target {
		t.Fatalf("card = %+v", ap)
	}
	h.approvals.ResolveFor(ap.ID, tools.LayerWorkspace)
	if out := <-done; !strings.Contains(out, "granted") || rl.get() == "" {
		t.Fatalf("result %q relaunch %q", out, rl.get())
	}
	e, _ := h.rt.agentEntry("scout")
	e.guard.Sandbox = rootsSandbox{}
	if sb := h.rt.spawnSandbox(e).(rootsSandbox); len(sb.extra) != 1 || sb.extra[0] != target {
		t.Fatalf("spawn roots = %v", sb.extra)
	}
	// Asking again for a folder inside it needs no card.
	out, err := ask(context.Background(), "write", filepath.Join(target, "sub"), "again")
	if err != nil || !strings.Contains(out, "already granted") {
		t.Fatalf("second ask = %q %v", out, err)
	}
}

// A sandbox denial in a run becomes the same card; the grant wakes the
// agent in the thread to retry.
func TestDenialOffersAccessAndContinues(t *testing.T) {
	h := episodeHarness(t)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout build"})
	target := realPath(t.TempDir())
	h.rt.offerAccess("scout", trig, target)
	h.rt.offerAccess("scout", trig, target) // deduped while pending
	ap := firstPending(t, h.approvals)
	if n := len(h.approvals.List()); n != 1 {
		t.Fatalf("%d cards for one denial", n)
	}
	h.approvals.ResolveFor(ap.ID, tools.LayerOnce)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range h.bus.History("#general", trig.ID) {
			if strings.HasPrefix(m.Text, "@scout continue: write access to "+target) {
				h.rt.WaitBackground()
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no continuation after the grant")
}

// Live (M32): codex could not run Homebrew's git-receive-pack, and the
// exec scope did not help — the sandbox's exec roots did not include it.
// Asking with a program path grants its folder read+execute, never write.
func TestPermissionRequestExecGrantsTheProgramsFolder(t *testing.T) {
	h := episodeHarness(t)
	_ = mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "push"})
	dir := realPath(t.TempDir())
	prog := filepath.Join(dir, "tool")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rl := &relaunch{}
	done := make(chan string, 1)
	go func() {
		out, err := h.rt.accessSeam("scout", rl)(context.Background(), "exec", prog, "push needs it")
		if err != nil {
			out = "ERR " + err.Error()
		}
		done <- out
	}()
	ap := firstPending(t, h.approvals)
	if ap.Describe() != "running programs from "+dir {
		t.Fatalf("card = %q", ap.Describe())
	}
	h.approvals.Decide(ap, "yes")
	if out := <-done; rl.get() == "" {
		t.Fatalf("result %q, no relaunch", out)
	}
	e, _ := h.rt.agentEntry("scout")
	e.guard.Sandbox = rootsSandbox{}
	sb := h.rt.spawnSandbox(e).(rootsSandbox)
	if len(sb.ro) != 1 || sb.ro[0] != dir || len(sb.extra) != 0 {
		t.Fatalf("roots rw=%v ro=%v", sb.extra, sb.ro)
	}
}

// A denial that names a program asks to run programs from its folder.
func TestExecDenialOffersTheProgramsFolder(t *testing.T) {
	h := episodeHarness(t)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout build"})
	dir := realPath(t.TempDir())
	tool := filepath.Join(dir, "tool")
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)
	h.rt.offerAccess("scout", trig, tool)
	ap := firstPending(t, h.approvals)
	if ap.Target != dir || !strings.Contains(ap.Reason, "run programs from") {
		t.Fatalf("card = %+v", ap)
	}
	h.approvals.ResolveFor(ap.ID, tools.LayerOnce)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range h.bus.History("#general", trig.ID) {
			if strings.HasPrefix(m.Text, "@scout continue: running programs from "+dir) {
				h.rt.WaitBackground()
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no continuation after the grant")
}
