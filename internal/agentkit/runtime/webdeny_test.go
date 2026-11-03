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

// hookStub copies the hooks.json of the --gemini_dir it was given into
// the dump dir, so a test can see what agy would have read.
const hookStub = `#!/bin/sh
for a in "$@"; do
  case "$a" in --gemini_dir=*) cp "${a#--gemini_dir=}/config/hooks.json" "$DUMPDIR/hooks.json" ;; esac
done
printf '%s\n' '{"type":"system","subtype":"init"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok","total_cost_usd":0}'
exit 0
`

// TestAntigravitySpawnWithoutToolsStillDeniesWeb pins the review fix: a
// spawn with no IDE-tools mirror (reflection, or an agent with no served
// tools) gets a deny-only mirror instead of being refused.
func TestAntigravitySpawnWithoutToolsStillDeniesWeb(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	doc := strings.Replace(baseDoc(), `runtime = "claude"`, `runtime = "antigravity"`, 1)
	h := newHarnessMulti(t, doc, map[string]string{"agy": hookStub})
	h.rt.cfg.Tuning = func() Tuning { return Tuning{MaxIterations: 3} }
	h.rt.mu.Lock()
	e := h.rt.agents["scout"]
	h.rt.mu.Unlock()
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout hi"})
	run, _ := h.rt.cliSpawnOnce(context.Background(), e, trig, spawnInput{
		prompt: "reflect", workdir: h.ws.Root, maxTurns: 1, quiet: true,
	})
	// The stub's stream is not agy's shape; only the refusal matters here.
	if strings.Contains(run.Error, "cannot be switched off") {
		t.Fatalf("spawn refused: %s", run.Error)
	}
	body, err := os.ReadFile(filepath.Join(h.dumpDir, "hooks.json"))
	if err != nil || !strings.Contains(string(body), "dhi-deny-native-web") {
		t.Fatalf("agy ran without the deny hook: %q %v", body, err)
	}
	if ents, _ := os.ReadDir(filepath.Join(h.ws.Root, ".dhi", "mcp")); len(ents) != 0 {
		t.Fatalf("deny-only mirror not cleaned up: %v", ents)
	}
}
