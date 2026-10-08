package kit

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Animation primitives (F-043). They are pure: no timers live here, the
// caller owns the clock and passes a frame index, so every render is
// deterministic under test. Under reduced motion (theme.Motion false,
// F-012) each primitive degrades to a static, complete rendering.

// SpinnerFrames are the activity-indicator frames; the frame index
// advances once per caller tick.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerGlyph renders the brand-colored spinner for frame, or the static
// busy glyph under reduced motion.
func SpinnerGlyph(frame int) string {
	if !theme.Motion {
		return theme.Brand().Render(theme.GlyphBusy)
	}
	n := len(SpinnerFrames)
	return theme.Brand().Render(SpinnerFrames[((frame%n)+n)%n])
}

// ProgressBar draws a determinate bar. Fraction is clamped to 0..1; the
// fill is a gradient between the accent tokens.
type ProgressBar struct {
	Width    int
	Fraction float64
}

// View renders the bar, exactly Width cells wide (min 4).
func (p ProgressBar) View() string {
	w := p.Width
	if w < 4 {
		w = 4
	}
	f := p.Fraction
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	filled := int(f*float64(w) + 0.5)
	var b strings.Builder
	for i := 0; i < filled; i++ {
		t := 0.0
		if w > 1 {
			t = float64(i) / float64(w-1)
		}
		col := theme.Blend(theme.Current.AccentDim, theme.Current.Accent, t)
		b.WriteString(lipgloss.NewStyle().Foreground(col).Render("█"))
	}
	b.WriteString(theme.TextDim().Render(strings.Repeat("░", w-filled)))
	return b.String()
}

// Typewriter returns the first n runes of text, so a caller revealing
// text over ticks just increments n. Under reduced motion the whole text
// is returned at once — nothing is hidden behind a clock that never runs.
func Typewriter(text string, n int) string {
	if !theme.Motion {
		return text
	}
	r := []rune(text)
	if n < 0 {
		n = 0
	}
	if n >= len(r) {
		return text
	}
	return string(r[:n])
}

// TypewriterDone reports whether n runes reveal all of text; callers stop
// their tick chain on it (always true under reduced motion).
func TypewriterDone(text string, n int) bool {
	return !theme.Motion || n >= len([]rune(text))
}

// StepState is a step's place in a wizard.
type StepState uint8

const (
	StepPending StepState = iota
	StepActive
	StepDone
	StepSkipped
)

// StepItem is one entry of a Stepper.
type StepItem struct {
	Label string
	State StepState
}

// Stepper renders wizard progress on one line:
//
//	✓ welcome › ◐ identity › ○ team
//
// When that does not fit width it collapses to "2/3 identity".
func Stepper(items []StepItem, width, frame int) string {
	sep := theme.TextDim().Render(" " + theme.GlyphChevron + " ")
	parts := make([]string, 0, len(items))
	for _, it := range items {
		switch it.State {
		case StepDone:
			parts = append(parts, theme.SuccessText().Render(theme.GlyphCheck)+" "+theme.TextStyle().Render(it.Label))
		case StepActive:
			parts = append(parts, SpinnerGlyph(frame)+" "+theme.Brand().Render(it.Label))
		case StepSkipped:
			parts = append(parts, theme.TextDim().Render("– "+it.Label))
		default:
			parts = append(parts, theme.TextDim().Render("○ "+it.Label))
		}
	}
	line := strings.Join(parts, sep)
	if width <= 0 || len([]rune(ansi.Strip(line))) <= width {
		return line
	}
	for i, it := range items {
		if it.State == StepActive {
			compact := theme.TextDim().Render(strconv.Itoa(i+1)+"/"+strconv.Itoa(len(items))+" ") + theme.Brand().Render(it.Label)
			return compact
		}
	}
	return ""
}
