package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestSpinnerGlyphAdvancesAndFreezesUnderReducedMotion(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	theme.MotionForTest(t, true)
	if ansi.Strip(SpinnerGlyph(0)) != SpinnerFrames[0] || ansi.Strip(SpinnerGlyph(3)) != SpinnerFrames[3] {
		t.Fatal("frames do not follow the index")
	}
	if ansi.Strip(SpinnerGlyph(len(SpinnerFrames)+2)) != SpinnerFrames[2] || ansi.Strip(SpinnerGlyph(-1)) == "" {
		t.Fatal("index must wrap, including negatives")
	}
	theme.MotionForTest(t, false)
	for _, f := range []int{0, 1, 7} {
		if ansi.Strip(SpinnerGlyph(f)) != theme.GlyphBusy {
			t.Fatalf("frame %d under reduced motion = %q", f, ansi.Strip(SpinnerGlyph(f)))
		}
	}
}

func TestProgressBarWidthAndClamp(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	for _, c := range []struct {
		f      float64
		filled int
	}{{-1, 0}, {0, 0}, {0.5, 5}, {1, 10}, {9, 10}} {
		out := ansi.Strip(ProgressBar{Width: 10, Fraction: c.f}.View())
		if len([]rune(out)) != 10 || strings.Count(out, "█") != c.filled {
			t.Errorf("f=%v → %q (want %d filled, 10 wide)", c.f, out, c.filled)
		}
	}
	if w := len([]rune(ansi.Strip(ProgressBar{Width: 1, Fraction: 1}.View()))); w != 4 {
		t.Errorf("min width = %d, want 4", w)
	}
}

func TestTypewriterReveal(t *testing.T) {
	theme.MotionForTest(t, true)
	if Typewriter("héllo", 2) != "hé" || Typewriter("héllo", 99) != "héllo" || Typewriter("x", -3) != "" {
		t.Fatal("reveal wrong")
	}
	if TypewriterDone("abc", 2) || !TypewriterDone("abc", 3) {
		t.Fatal("done wrong")
	}
	theme.MotionForTest(t, false)
	if Typewriter("hello", 0) != "hello" || !TypewriterDone("hello", 0) {
		t.Fatal("reduced motion must show everything at once")
	}
}

func steps() []StepItem {
	return []StepItem{
		{"welcome", StepDone}, {"workspace", StepSkipped},
		{"identity", StepActive}, {"conventions", StepPending}, {"done", StepPending},
	}
}

func TestStepperCollapsesWhenNarrow(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	theme.MotionForTest(t, true)
	wide := ansi.Strip(Stepper(steps(), 120, 0))
	for _, want := range []string{"✓ welcome", "– workspace", "identity", "○ conventions"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide stepper lacks %q: %q", want, wide)
		}
	}
	if got := ansi.Strip(Stepper(steps(), 30, 0)); got != "3/5 identity" {
		t.Errorf("narrow stepper = %q", got)
	}
}

func TestGoldenStepperMotion(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	theme.MotionForTest(t, true)
	golden.Snapshot(t, "stepper_motion_f2", Stepper(steps(), 120, 2))
	theme.MotionForTest(t, false)
	golden.Snapshot(t, "stepper_reduced", Stepper(steps(), 120, 2))
}

func TestGoldenProgress(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	var b strings.Builder
	for _, f := range []float64{0, 0.25, 0.6, 1} {
		b.WriteString(ProgressBar{Width: 24, Fraction: f}.View() + "\n")
	}
	golden.Snapshot(t, "progress_bars", strings.TrimSuffix(b.String(), "\n"))
}
