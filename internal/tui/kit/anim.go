package kit

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Animation primitives (F-043). They are pure: no timers live here, the
// caller owns the clock and passes a frame index, so every render is
// deterministic under test. Under reduced motion (theme.Motion false,
// F-012) each primitive degrades to a static, complete rendering.

// SpinnerFrames are the activity-indicator frames — a breathing dot; the
// frame index advances once per caller tick. Braille spinners render as
// boxes in common fonts (F-093), so the pulse uses ·, ◦ and •; the
// static reduced-motion glyph (theme.GlyphBusy, ●) is never a frame.
var SpinnerFrames = []string{"·", "◦", "•", "•", "◦", "·"}

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
//	✓ welcome ── ● identity ── ○ team
//
// The connector is green behind finished steps and a quiet rule ahead
// (F-093). When the whole bar does not fit width it shows a window of
// steps around the active one — "3/10 … ✓ project ── ● identity ── ○ you …"
// — so every visible step can still be clicked (F-108); only when not
// even one step fits does it fall back to "2/3 identity".
func Stepper(items []StepItem, width, frame int) string {
	done := theme.SuccessText().Render(" ── ")
	ahead := theme.RuleText().Render(" ── ")
	parts := make([]string, len(items))
	widths := make([]int, len(items))
	active := 0
	for i, it := range items {
		switch it.State {
		case StepDone:
			parts[i] = theme.SuccessText().Render(theme.GlyphCheck) + " " + theme.TextStyle().Render(it.Label)
		case StepActive:
			parts[i] = SpinnerGlyph(frame) + " " + theme.Brand().Render(it.Label)
			active = i
		case StepSkipped:
			parts[i] = theme.TextDim().Render("– " + it.Label)
		default:
			parts[i] = theme.TextDim().Render("○ " + it.Label)
		}
		widths[i] = 2 + len([]rune(it.Label))
	}
	join := func(from, to int) string {
		var line string
		for i := from; i < to; i++ {
			if i > from {
				if items[i-1].State == StepDone || items[i-1].State == StepSkipped {
					line += done
				} else {
					line += ahead
				}
			}
			line += parts[i]
		}
		return line
	}
	span := func(from, to int) int {
		w := 0
		for i := from; i < to; i++ {
			w += widths[i]
			if i > from {
				w += 4
			}
		}
		return w
	}
	if width <= 0 || span(0, len(items)) <= width {
		return join(0, len(items))
	}
	if len(items) == 0 {
		return ""
	}
	prefix := strconv.Itoa(active+1) + "/" + strconv.Itoa(len(items)) + " "
	fits := func(from, to int) bool {
		w := len(prefix) + span(from, to)
		if from > 0 {
			w += 2 // "… "
		}
		if to < len(items) {
			w += 2 // " …"
		}
		return w <= width
	}
	from, to := active, active+1
	if !fits(from, to) {
		return theme.TextDim().Render(prefix) + theme.Brand().Render(items[active].Label)
	}
	// Earlier steps first: they are the ones a click goes back to.
	for grew := true; grew; {
		grew = false
		if from > 0 && fits(from-1, to) {
			from, grew = from-1, true
		}
		if to < len(items) && fits(from, to+1) {
			to, grew = to+1, true
		}
	}
	line := theme.TextDim().Render(prefix)
	if from > 0 {
		line += theme.TextDim().Render("… ")
	}
	line += join(from, to)
	if to < len(items) {
		line += theme.TextDim().Render(" …")
	}
	return line
}
