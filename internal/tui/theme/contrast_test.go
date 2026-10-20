package theme

import (
	"image/color"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/lucasb-eyer/go-colorful"
)

// TestTextTokensMeetWCAGAA pins readability: every text-bearing token must
// reach 4.5:1 on every surface it can sit on (the hint bar and selected
// rows are the hardest). AccentDim is secondary emphasis, held to 3:1.
func TestTextTokensMeetWCAGAA(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		surfaces := map[string]color.Color{
			"Bg": tk.Bg, "BgPanel": tk.BgPanel, "BgElevated": tk.BgElevated,
			"BgInset": tk.BgInset, "BgChrome": tk.BgChrome, "BgSelection": tk.BgSelection,
			"BgHeader": tk.BgHeader, "BgRowAlt": tk.BgRowAlt, "BgCursorLine": tk.BgCursorLine,
		}
		texts := map[string]struct {
			c   color.Color
			min float64
		}{
			"Text": {tk.Text, 4.5}, "TextDim": {tk.TextDim, 4.5}, "TextMuted": {tk.TextMuted, 4.5},
			"Accent": {tk.Accent, 4.5}, "Accent2": {tk.Accent2, 4.5}, "Info": {tk.Info, 4.5},
			"Accent3": {tk.Accent3, 4.5}, "Accent4": {tk.Accent4, 4.5},
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

// Code is read ON the diff washes: every colour the syntax highlighter uses
// must stay legible there (F-049). The stronger changed-word wash carries
// plain Text only (the renderer drops syntax colour inside it).
func TestCodeIsLegibleOnTheDiffWashes(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		light := map[string]color.Color{"BgAdd": tk.BgAdd, "BgDel": tk.BgDel}
		texts := map[string]struct {
			c   color.Color
			min float64
		}{
			"Text": {tk.Text, 4.5}, "TextMuted(comment)": {tk.TextMuted, 4.4},
			"Accent": {tk.Accent, 4.5}, "Accent2(keyword)": {tk.Accent2, 4.5},
			"Info(number)": {tk.Info, 4.5}, "Success(string)": {tk.Success, 4.5},
			"AccentDim(operator)": {tk.AccentDim, 3.0},
		}
		for wn, bg := range light {
			for tn, tx := range texts {
				if got := ratio(tx.c, bg); got < tx.min {
					t.Errorf("%s: %s on %s = %.2f:1, want >= %.1f", tk.Name, tn, wn, got, tx.min)
				}
			}
		}
		for wn, bg := range map[string]color.Color{"BgAddStrong": tk.BgAddStrong, "BgDelStrong": tk.BgDelStrong} {
			if got := ratio(tk.Text, bg); got < 7 {
				t.Errorf("%s: Text on %s = %.2f:1, want >= 7", tk.Name, wn, got)
			}
		}
	}
}

// The strong wash must read as a deeper step of its wash, not a new hue.
func TestStrongWashIsDeeperThanItsWash(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		if ratio(tk.BgAddStrong, tk.BgAdd) < 1.25 || ratio(tk.BgDelStrong, tk.BgDel) < 1.25 {
			t.Errorf("%s: the changed-word wash is not distinguishable from the line wash", tk.Name)
		}
	}
}

// F-064 pills: every semantic and accent color stays legible on its own
// tint in all three themes.
func TestPillsAreLegible(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		old := Current
		Current = tk
		for name, c := range map[string]color.Color{
			"Accent": tk.Accent, "Accent2": tk.Accent2, "Accent3": tk.Accent3, "Accent4": tk.Accent4,
			"Info": tk.Info, "Success": tk.Success, "Warning": tk.Warning, "Danger": tk.Danger,
			"TextDim": tk.TextDim,
		} {
			if got := ratio(c, PillBg(c)); got < 4.5 {
				t.Errorf("%s: %s pill = %.2f:1, want >= 4.5", tk.Name, name, got)
			}
		}
		Current = old
	}
}

// TestModeBlockIsLegible: the status row's solid view block (ADR-0040)
// keeps its label at AA on every view's hue in every theme.
func TestModeBlockIsLegible(t *testing.T) {
	defer SetSurface(surface)
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		for _, id := range []string{"workspace", "editor", "ideator", "reviewer", "settings"} {
			old := Current
			Current = tk
			SetSurface(id)
			fill := ModeFill()
			if got := ratio(OnFill(fill), fill); got < 4.5 {
				t.Errorf("%s/%s: mode block text = %.2f:1, want >= 4.5", tk.Name, id, got)
			}
			Current = old
		}
	}
}

// TestRailActiveIsLegible: the rail's highlighted row (F-093) reads at
// AA on the selection shade in every view and theme.
func TestRailActiveIsLegible(t *testing.T) {
	defer SetSurface(surface)
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		for _, id := range []string{"workspace", "editor", "ideator", "reviewer", "settings"} {
			old := Current
			Current = tk
			SetSurface(id)
			if got := ratio(SurfaceText(), tk.BgSelection); got < 4.5 {
				t.Errorf("%s/%s: rail active = %.2f:1, want >= 4.5", tk.Name, id, got)
			}
			Current = old
		}
	}
}

// TestShadeLadderSurvives256Colors: after a 256-color downsample the
// rail (inset) and the pane (panel) stay distinct, and the selection
// stays visible on every zone. The dark canvas and panel DO collapse
// there; that boundary is carried by rows (tab bar, status row) and the
// pane's divider and header rule, not by shade (ADR-0040).
func TestShadeLadderSurvives256Colors(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light()} {
		conv := func(c color.Color) color.Color { return colorprofile.ANSI256.Convert(c) }
		pairs := [][2]color.Color{{tk.Bg, tk.BgSelection}, {tk.BgPanel, tk.BgSelection}, {tk.BgInset, tk.BgSelection},
			{tk.BgPanel, tk.BgInset}}
		for i, p := range pairs {
			a, b := conv(p[0]), conv(p[1])
			ar, ag, ab, _ := a.RGBA()
			br, bg, bb, _ := b.RGBA()
			if ar == br && ag == bg && ab == bb {
				t.Errorf("%s: pair %d collapses to one color in 256-color mode", tk.Name, i)
			}
		}
	}
}

// minBrandDelta is the least CIEDE2000 distance (×100) between any two
// of the brand and semantic hues: far enough that saffron never reads
// as Warning and a status never reads as the brand (ADR-0041).
const minBrandDelta = 15.0

func TestBrandAndSemanticsAreDistinct(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		hues := []struct {
			n string
			c color.Color
		}{{"Primary", tk.Accent}, {"Secondary", tk.Accent2}, {"Success", tk.Success},
			{"Warning", tk.Warning}, {"Danger", tk.Danger}}
		for i := range hues {
			for j := i + 1; j < len(hues); j++ {
				a, _ := colorful.MakeColor(hues[i].c)
				b, _ := colorful.MakeColor(hues[j].c)
				if d := a.DistanceCIEDE2000(b) * 100; d < minBrandDelta {
					t.Errorf("%s: %s and %s are too close (ΔE00 %.1f, want >= %.0f)",
						tk.Name, hues[i].n, hues[j].n, d, minBrandDelta)
				}
			}
		}
	}
}

// TestComponentRolesAreLegible: every control's text reads at AA on its
// own surface in every theme (F-096).
func TestComponentRolesAreLegible(t *testing.T) {
	for _, tk := range []Tokens{Dark(), Light(), HighContrast()} {
		old := Current
		Current = tk
		check := func(name string, fg, bg color.Color) {
			if got := ratio(fg, bg); got < 4.5 {
				t.Errorf("%s: %s = %.2f:1, want >= 4.5", tk.Name, name, got)
			}
		}
		for _, v := range []ButtonVariant{ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost} {
			st := Button(v, false, false)
			check("button "+itoa(int(v)), st.GetForeground(), st.GetBackground())
			fst := Button(v, true, false)
			check("focused button "+itoa(int(v)), fst.GetForeground(), fst.GetBackground())
		}
		check("disabled button", Button(ButtonPrimary, false, true).GetForeground(), tk.BgInput)
		check("input text", tk.Text, tk.BgInput)
		check("placeholder", tk.TextMuted, tk.BgInput)
		check("card title", tk.Text, tk.BgCard)
		check("card selected title", SurfaceText(), tk.BgCard)
		check("card text", tk.TextDim, tk.BgCard)
		check("link", tk.Accent2, tk.BgPanel)
		for _, k := range []BadgeKind{BadgeNeutral, BadgeOK, BadgeWarn, BadgeError, BadgeInfo, BadgeBusy} {
			st := Badge(k)
			check("badge "+itoa(int(k)), st.GetForeground(), st.GetBackground())
		}
		tab := TabCurrent()
		check("active tab", tab.GetForeground(), tab.GetBackground())
		// The input and card wells must be told apart from the panel
		// (UI boundary, 1.1:1 is the shade ladder's own step).
		if ratio(tk.BgInput, tk.BgPanel) < 1.08 && tk.Name != HighContrast().Name {
			t.Errorf("%s: input well does not stand off the panel", tk.Name)
		}
		Current = old
	}
}
