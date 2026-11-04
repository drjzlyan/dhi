package runtime

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// Only a fresh, successful spawn with a reported per-call context counts.
func TestCalibrateSamplesOnlyFreshReportedRuns(t *testing.T) {
	h := newHarness(t, baseDoc())
	e := h.rt.agents["scout"]
	key := calKey(e.cli.Name, e.m.CLIModel())
	n := func() float64 {
		h.rt.cal.mu.Lock()
		defer h.rt.cal.mu.Unlock()
		return h.rt.cal.m[key].N
	}
	ok := tasks.Run{Status: tasks.RunOK, Context: 9000}
	trig := bus.Message{Channel: "#general"}
	h.rt.calibrate(trig, e, episode.Episode{}, ok, false, 20000) // resumed
	h.rt.calibrate(trig, e, episode.Episode{}, tasks.Run{Status: tasks.RunOK}, true, 20000)
	h.rt.calibrate(trig, e, episode.Episode{}, tasks.Run{Status: tasks.RunError, Context: 9000}, true, 20000)
	if n() != 0 {
		t.Fatalf("sampled %v runs that say nothing about the prompt", n())
	}
	h.rt.calibrate(trig, e, episode.Episode{}, ok, true, 20000)
	if n() != 1 {
		t.Fatalf("samples = %v, want 1", n())
	}
	h.rt.calibrate(trig, e, episode.Episode{Model: "other"}, ok, true, 20000) // keyed by the conversation's model
	if n() != 1 {
		t.Fatalf("a model override must calibrate its own key, base N = %v", n())
	}
}

// feed observes prompts of the given sizes that cost runes/ratio+overhead tokens.
func feed(t *testing.T, c *calibration, key [2]string, ratio float64, overhead int, sizes ...int) {
	t.Helper()
	for _, n := range sizes {
		if err := c.Observe(key[0], key[1], n, int(float64(n)/ratio)+overhead); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCalibrationFitsSlopeNotOverhead(t *testing.T) {
	c := newCalibration(t.TempDir())
	k := [2]string{"claude", "opus"}
	if got := c.Ratio("claude", "opus"); got != runesPerToken {
		t.Fatalf("no data = %v, want the default %d", got, runesPerToken)
	}
	// The CLI's own 15k-token overhead must not read as dense text.
	feed(t, c, k, 3, 15000, 6000, 20000, 9000, 40000, 12000, 30000)
	if got := c.Ratio("claude", "opus"); math.Abs(got-3) > 0.05 {
		t.Fatalf("ratio = %v, want ~3 (overhead separated)", got)
	}
	if got := c.Ratio("claude", "haiku"); got != runesPerToken {
		t.Fatalf("another model must keep the default, got %v", got)
	}
}

func TestCalibrationNeedsDataAndSpread(t *testing.T) {
	c := newCalibration(t.TempDir())
	k := [2]string{"codex", "m"}
	feed(t, c, k, 2, 0, 10000, 30000) // two samples: not enough
	if got := c.Ratio("codex", "m"); got != runesPerToken {
		t.Fatalf("two samples = %v, want the default", got)
	}
	c2 := newCalibration(t.TempDir())
	feed(t, c2, k, 2, 0, 10000, 10000, 10000, 10000, 10000) // same size every time
	if got := c2.Ratio("codex", "m"); got != runesPerToken {
		t.Fatalf("no spread = %v, want the default", got)
	}
	if err := c2.Observe("codex", "m", 100, 50); err != nil { // too small to count
		t.Fatal(err)
	}
}

func TestCalibrationClampsToSaneBounds(t *testing.T) {
	sizes := []int{6000, 20000, 9000, 40000, 12000}
	low := newCalibration(t.TempDir())
	feed(t, low, [2]string{"a", "b"}, 1, 0, sizes...)
	if got := low.Ratio("a", "b"); got != minRunesPerToken {
		t.Fatalf("dense = %v, want clamp %v", got, minRunesPerToken)
	}
	high := newCalibration(t.TempDir())
	feed(t, high, [2]string{"a", "b"}, 20, 0, sizes...)
	if got := high.Ratio("a", "b"); got != maxRunesPerToken {
		t.Fatalf("sparse = %v, want clamp %v", got, maxRunesPerToken)
	}
}

func TestCalibrationPersistsInDHIFolder(t *testing.T) {
	root := t.TempDir()
	c := newCalibration(root)
	feed(t, c, [2]string{"claude", "m"}, 3, 5000, 6000, 20000, 9000, 40000)
	if _, err := os.Stat(filepath.Join(root, ".dhi", "calibration.json")); err != nil {
		t.Fatal(err)
	}
	if got := newCalibration(root).Ratio("claude", "m"); math.Abs(got-3) > 0.05 {
		t.Fatalf("reloaded ratio = %v, want ~3", got)
	}
}

func TestCalibrationCorruptFileKeepsDefaultAndNamesError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dhi", "calibration.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newCalibration(root)
	if got := c.Ratio("a", "b"); got != runesPerToken {
		t.Fatalf("ratio = %v", got)
	}
	if err := c.Observe("a", "b", 5000, 1000); err == nil {
		t.Fatal("an unreadable calibration file must be reported, not overwritten silently")
	}
}

func TestPromptBudgetUsesCalibratedRatio(t *testing.T) {
	h := newHarness(t, baseDoc())
	for i := 0; i < 120; i++ {
		mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "decision " + strings.Repeat("detail ", 60) + ". more"})
	}
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Resume: true, MaxIterations: 3, Memory: MemoryTuning{PromptTokens: 6000}} }
	e := h.rt.agents["scout"]
	size := func() int {
		p, s := h.rt.cliPromptAt(context.Background(), e, trig, "", 0, nil, false)
		return runeLen(p) + runeLen(s)
	}
	def := size()
	if def > 6000*runesPerToken || def < 6000*runesPerToken/2 {
		t.Fatalf("default fit = %d runes, want near %d", def, 6000*runesPerToken)
	}
	feed(t, h.rt.cal, [2]string{e.cli.Name, e.m.CLIModel()}, 2, 4000, 6000, 20000, 9000, 40000, 12000)
	if got := size(); got > 6000*2 || got >= def {
		t.Fatalf("calibrated fit = %d runes, want <= %d (and below the default %d)", got, 6000*2, def)
	}
}
