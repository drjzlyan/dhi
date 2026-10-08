package theme

import (
	"image/color"
	"math"
	"testing"
)

// relLum is the WCAG 2.x relative luminance of c.
func relLum(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		f := float64(v) / 65535
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func ratio(a, b color.Color) float64 {
	la, lb := relLum(a), relLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestTextTokensMeetWCAGAA pins readability: every text-bearing token must
// reach 4.5:1 on every surface it can sit on (the hint bar and selected
// rows are the hardest). AccentDim is secondary emphasis, held to 3:1.
func TestTextTokensMeetWCAGAA(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		surfaces := map[string]color.Color{
			"Bg": tk.Bg, "BgPanel": tk.BgPanel, "BgElevated": tk.BgElevated,
			"BgInset": tk.BgInset, "BgChrome": tk.BgChrome, "BgSelection": tk.BgSelection,
		}
		texts := map[string]struct {
			c   color.Color
			min float64
		}{
			"Text": {tk.Text, 4.5}, "TextDim": {tk.TextDim, 4.5}, "TextMuted": {tk.TextMuted, 4.5},
			"Accent": {tk.Accent, 4.5}, "Accent2": {tk.Accent2, 4.5}, "Info": {tk.Info, 4.5},
			"Success": {tk.Success, 4.5}, "Warning": {tk.Warning, 4.5}, "Danger": {tk.Danger, 4.5},
			"AccentDim": {tk.AccentDim, 3.0},
		}
		for tn, tx := range texts {
			for sn, bg := range surfaces {
				if got := ratio(tx.c, bg); got < tx.min {
					t.Errorf("%s: %s on %s = %.2f:1, want >= %.1f", tk.Name, tn, sn, got, tx.min)
				}
			}
		}
	}
}

// TestTextHierarchyHolds keeps Text > TextDim > TextMuted in luminance
// contrast, so emphasis levels stay distinguishable after the AA raise.
func TestTextHierarchyHolds(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		a, b, c := ratio(tk.Text, tk.Bg), ratio(tk.TextDim, tk.Bg), ratio(tk.TextMuted, tk.Bg)
		if !(a > b && b > c) {
			t.Errorf("%s: contrast on Bg Text %.2f / Dim %.2f / Muted %.2f is not strictly decreasing", tk.Name, a, b, c)
		}
	}
}

// TestHighContrastBordersAreVisible: the accessibility theme's panel edges
// must read against the canvas (3:1 for UI boundaries).
func TestHighContrastBordersAreVisible(t *testing.T) {
	hc := HighContrast()
	if got := ratio(hc.Border, hc.Bg); got < 3 {
		t.Errorf("border %.2f:1 on Bg, want >= 3", got)
	}
	if got := ratio(hc.BorderFocused, hc.Bg); got < 7 {
		t.Errorf("focused border %.2f:1 on Bg, want >= 7", got)
	}
}
