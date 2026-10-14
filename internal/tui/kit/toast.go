package kit

import (
	"image/color"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ToastKind picks a toast's accent.
type ToastKind int

const (
	ToastSuccess ToastKind = iota
	ToastInfo
	ToastWarning
)

// Toast is a short-lived announcement that floats top-right over the
// app (F-093): an outcome worth a moment — a task shipped — never an
// instruction. The shell owns its lifetime; the surface only raises it.
type Toast struct {
	Kind   ToastKind
	Title  string // "ship-docs shipped"
	Detail string // "3 runs · $0.42"; optional
}

func (t Toast) accent() color.Color {
	switch t.Kind {
	case ToastInfo:
		return theme.Current.Info
	case ToastWarning:
		return theme.Current.Warning
	}
	return theme.Current.Success
}

// View renders the toast as one elevated block, at most width cells:
// an accent bar, the glyph and title, and the muted detail. Reveal is
// the slide-in progress (0..1); under reduced motion callers pass 1.
func (t Toast) View(width int, reveal float64) string {
	glyph := theme.GlyphCheck
	switch t.Kind {
	case ToastInfo:
		glyph = theme.GlyphSpark
	case ToastWarning:
		glyph = "!"
	}
	bg := theme.ElevatedBg()
	bar := bg.Foreground(t.accent()).Render("▌")
	head := bg.Foreground(t.accent()).Bold(true).Render(" " + glyph + " " + t.Title)
	row := bar + head
	if t.Detail != "" {
		row += bg.Foreground(theme.Current.TextDim).Render("  " + t.Detail)
	}
	row += bg.Render(" ")
	w := min(runeWidth(row), width)
	if reveal < 1 {
		w = max(int(float64(w)*reveal), 1)
	}
	return PaintRow(ClipEllipsis(row, w), w, bg)
}
