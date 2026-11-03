package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
)

// TestNativeWebIsDeniedUnlessAllowed pins F-123 end to end: the spawn's
// argv carries the deny flag by default and drops it when the setting
// allows the CLI's own web tools.
func TestNativeWebIsDeniedUnlessAllowed(t *testing.T) {
	for name, tc := range map[string]struct {
		tune Tuning
		want bool
	}{
		"default (no tuning)": {tune: Tuning{}, want: true},
		"allowed":             {tune: Tuning{AllowNativeWeb: true}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, baseDoc())
			tune := tc.tune
			tune.MaxIterations = 3
			h.rt.cfg.Tuning = func() Tuning { return tune }
			replies, cancel := h.bus.Subscribe("#general")
			defer cancel()
			trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout hi"})
			h.rt.Handle(context.Background(), trig)
			waitReply(t, replies)
			got := slices.Contains(readArgs(t, h), "--disallowedTools")
			if got != tc.want {
				t.Fatalf("--disallowedTools present = %v, want %v:\n%s", got, tc.want, joinArgs(t, h))
			}
		})
	}
}

func TestDenyWebEnvFollowsTheSetting(t *testing.T) {
	h := newHarness(t, baseDoc())
	oc, ok := clirun.NewRegistry(nil).Get("opencode")
	if !ok {
		t.Fatal("no opencode adapter")
	}
	has := func() bool {
		for _, kv := range h.rt.cliEnv(oc, "scout", "") {
			if strings.HasPrefix(kv, "OPENCODE_PERMISSION=") {
				return true
			}
		}
		return false
	}
	h.rt.cfg.Tuning = func() Tuning { return Tuning{} }
	if !has() {
		t.Fatal("default must deny")
	}
	h.rt.cfg.Tuning = func() Tuning { return Tuning{AllowNativeWeb: true} }
	if has() {
		t.Fatal("allowed: DHI must not set the deny")
	}
}

// TestAntigravityTurnWithoutAMirrorIsRefusedWhenDenying pins the ADR-0011
// edge of F-123: antigravity's denial rides its per-turn config dir, so a
// turn that would run without one stops by name instead of running with
// the web tools on.
func TestAntigravityTurnWithoutAMirrorIsRefusedWhenDenying(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	doc := strings.Replace(baseDoc(), `runtime = "claude"`, `runtime = "antigravity"`, 1)
	h := newHarnessMulti(t, doc, map[string]string{"agy": silentStub})
	h.rt.cfg.Tuning = func() Tuning { return Tuning{MaxIterations: 3} }
	// A file where the per-turn config dir belongs: the mirror cannot be built.
	if err := os.MkdirAll(filepath.Join(h.ws.Root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.ws.Root, ".dhi", "mcp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout hi"})
	h.rt.Handle(context.Background(), trig)
	var all []string
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-replies:
			all = append(all, m.Text)
			if strings.Contains(m.Text, "cannot be switched off") {
				return
			}
		case <-deadline:
			t.Fatalf("no refusal notice; saw %q", all)
		}
	}
}
