// Package theme defines the single source of truth for DHI's visual identity.
//
// Every color, spacing value and glyph used anywhere in the TUI must come from
// here. A lint test (see theme_lint_test.go) fails the build if raw colors
// appear outside this package, keeping branding consistent across all
// components and surfaces.
package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// Tokens is the complete design-token set for one DHI theme.
type Tokens struct {
	// Name identifies the theme (shown in doctor output, config, etc).
	Name string

	// Palette.
	Bg            color.Color // application background
	BgPanel       color.Color // panel/card background
	BgElevated    color.Color // overlays, modals, popups
	BgInset       color.Color // sub-columns inside a panel: board columns, chat rail, thread pane
	BgChrome      color.Color // bottom chrome: hint bars, keymap rows (F-025)
	BgSelection   color.Color // selected rows, highlighted ranges
	BgOverlay     color.Color // modal backdrop dim layer
	Border        color.Color // unfocused borders, dividers
	BorderFocused color.Color // focused element borders
	Text          color.Color // primary text
	TextDim       color.Color // secondary text
	TextMuted     color.Color // hints, disabled elements
	Accent        color.Color // primary brand accent (cyan)
	Accent2       color.Color // secondary brand accent (violet)
	Success       color.Color
	Warning       color.Color
	Danger        color.Color

	// Layout metrics (in terminal cells).
	PadX        int // horizontal padding inside panels
	RadiusPad   int // breathing room around the app frame
	HeightTab   int // tab bar height
	HeightState int // statusline height
}

// Dark is the default DHI theme: near-black canvas with a cyan-to-violet
// futuristic accent ramp.
func Dark() Tokens {
	c := lipgloss.Color
	return Tokens{
		Name: "dark-futuristic",

		Bg:            c("#0B0E14"),
		BgPanel:       c("#10141B"),
		BgElevated:    c("#151B26"),
		BgInset:       c("#07090D"),
		BgChrome:      c("#262F3E"),
		BgSelection:   c("#1B2739"),
		BgOverlay:     c("#05070B"),
		Border:        c("#232C3B"),
		BorderFocused: c("#22D3EE"),
		Text:          c("#E6EDF3"),
		TextDim:       c("#8B98A9"),
		TextMuted:     c("#58657A"),
		Accent:        c("#22D3EE"),
		Accent2:       c("#A78BFA"),
		Success:       c("#34D399"),
		Warning:       c("#FBBF24"),
		Danger:        c("#F87171"),

		PadX:        1,
		RadiusPad:   0,
		HeightTab:   1,
		HeightState: 1,
	}
}

// Light is the daylight alternative: warm paper canvas with the same
// cyan-to-violet accents darkened for contrast.
func Light() Tokens {
	c := lipgloss.Color
	return Tokens{
		Name: "light-paper",

		Bg:            c("#F5F2EA"),
		BgPanel:       c("#FBF9F3"),
		BgElevated:    c("#FFFFFF"),
		BgInset:       c("#EDE9DE"),
		BgChrome:      c("#E6E0D0"),
		BgSelection:   c("#DCEFEF"),
		BgOverlay:     c("#E3DED0"),
		Border:        c("#D8D2C4"),
		BorderFocused: c("#0E7490"),
		Text:          c("#1F2937"),
		TextDim:       c("#5B6472"),
		TextMuted:     c("#8B93A1"),
		Accent:        c("#0E7490"),
		Accent2:       c("#6D28D9"),
		Success:       c("#047857"),
		Warning:       c("#B45309"),
		Danger:        c("#B91C1C"),

		PadX:        1,
		RadiusPad:   0,
		HeightTab:   1,
		HeightState: 1,
	}
}

// Current is the active theme. Swappable at runtime from Settings; everything
// must read styles through the helpers below rather than caching
// colors directly.
var Current = Dark()

// Motion gates animated effects (spinner frames, view transitions).
// Set by settings.Apply from the reduced_motion preference; everything
// reads this rather than caching, so a live Settings toggle takes
// effect immediately (F-012).
var Motion = true

// SetMotion flips the reduced-motion switch (production path: settings).
func SetMotion(on bool) { Motion = on }

// MotionForTest installs on for the duration of the test.
func MotionForTest(t interface{ Cleanup(func()) }, on bool) {
	old := Motion
	Motion = on
	t.Cleanup(func() { Motion = old })
}

// SwapForTest installs tk for the duration of the test and restores it via
// t.Cleanup. Rendering helpers read Current lazily so swaps take effect.
func SwapForTest(t interface{ Cleanup(func()) }, tk Tokens) {
	old := Current
	Current = tk
	t.Cleanup(func() { Current = old })
}

// ---------------------------------------------------------------------------
// Style helpers. Components build on these so re-theming never touches
// component code.
// ---------------------------------------------------------------------------

func base() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.Bg).Foreground(Current.Text)
}

// AppFrame styles the outermost background layer.
func AppFrame() lipgloss.Style { return base() }

// PanelBorder returns the rounded border used by every panel.
func PanelBorder(focused bool) lipgloss.Border {
	b := lipgloss.RoundedBorder()
	if !focused {
		return b
	}
	return b
}

// PanelEdge styles border glyphs (edges are painted cell-by-cell by kit.Panel,
// not via lipgloss Border, so widths stay exact).
func PanelEdge(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(panelEdge(focused))
}

// PanelBg paints panel body cells.
func PanelBg() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgPanel)
}

// InsetBg paints sub-columns inside a panel — board columns, the chat
// rail, the thread pane. One shade darker than the panel so nesting
// reads as depth, not clutter (F-024).
func InsetBg() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgInset)
}

// OverlayDim paints the modal backdrop: content dimmed under a solid
// near-black veil.
func OverlayDim() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgOverlay)
}

// ElevatedBg paints modal and context-pane bodies.
func ElevatedBg() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgElevated)
}

// Chip styles a quiet status pill: selection background, dim text.
// Callers colorize with theme.Current.Success/Warning/Danger when the
// status warrants it.
func Chip() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgSelection).Foreground(Current.TextDim)
}

// ChromeBar is the bottom hint-bar base: chrome background, dim text
// (F-025 Part A — instructions live here, never the focus).
func ChromeBar() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgChrome).Foreground(Current.TextDim)
}

// ChromeStatus styles one status/flash segment on the chrome bar;
// pass theme.Current.Success/Warning/Danger (or Text) for the fg.
func ChromeStatus(fg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgChrome).Foreground(fg)
}

// RailDim / RailMuted style inactive rail rows on the inset
// background (single style per row — SGR resets inside concatenated
// segments drop the row background).
func RailDim() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgInset).Foreground(Current.TextDim)
}

func RailMuted() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgInset).Foreground(Current.TextMuted)
}

func panelEdge(focused bool) color.Color {
	if focused {
		return Current.BorderFocused
	}
	return Current.Border
}

// PanelTitle styles panel titles rendered onto top borders.
func PanelTitle(focused bool) lipgloss.Style {
	fg := Current.TextDim
	if focused {
		fg = Current.Text
	}
	return lipgloss.NewStyle().Foreground(fg).Bold(focused)
}

// TabBar styles the top navigation bar container.
func TabBar() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.Bg).
		Foreground(Current.TextDim)
}

// TabActive / TabInactive style individual tabs.
func TabActive() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(Current.BgSelection).
		Foreground(Current.Accent).
		Bold(true)
}

func TabInactive() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.TextDim)
}

// StatusBar styles the bottom statusline container.
func StatusBar() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.Bg).Foreground(Current.TextDim)
}

// Brand styles the DHI wordmark.
func Brand() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Accent).Bold(true)
}

// Hint styles keyboard hint fragments ("1-9 switch").
func Hint() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.TextMuted) }

// HelpOverlay styles the help modal box.
func HelpOverlay() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(Current.Accent2).
		Background(Current.BgElevated).
		Padding(1, 2)
}

// Success / Warning / Danger semantic text styles.
func SuccessText() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.Success) }
func WarningText() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.Warning) }
func DangerText() lipgloss.Style  { return lipgloss.NewStyle().Foreground(Current.Danger) }

// TextDim styles secondary text.
func TextDim() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.TextDim) }

// TextMuted styles hints, disabled and placeholder text.
func TextMuted() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.TextMuted) }

// DialogEdge / DialogTitle style modal boxes (violet edge = dialog
// identity, distinct from focused panes).
func DialogEdge() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Accent2)
}

func DialogTitle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Text).Bold(true)
}

// Faint dims a pre-rendered block of text (view-transition fade-in,
// F-012). Content is byte-identical: lines are styled one at a time so
// lipgloss never re-pads them to a common width, and the terminal
// degrades the SGR hint gracefully when it cannot honor it.
func Faint(s string) string {
	st := lipgloss.NewStyle().Faint(true)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = st.Render(l)
	}
	return strings.Join(lines, "\n")
}

// Blend interpolates between two brand colors; t=0 → a, t=1 → b.
func Blend(a, b color.Color, t float64) color.Color {
	cf := colorful.Color{R: colR(a), G: colG(a), B: colB(a)}
	ct := colorful.Color{R: colR(b), G: colG(b), B: colB(b)}
	out := cf.BlendLuv(ct, clamp01(t))
	return lipgloss.Color(out.Hex())
}

func clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

func colR(c color.Color) float64 { r, _, _, _ := c.RGBA(); return f32(r) }
func colG(c color.Color) float64 { _, g, _, _ := c.RGBA(); return f32(g) }
func colB(c color.Color) float64 { _, _, b, _ := c.RGBA(); return f32(b) }

func f32(v uint32) float64 { return float64(v>>8) / 255 }

// ---------------------------------------------------------------------------
// Glyphs. Nerd Font glyphs degrade gracefully to boxes on plain fonts;
// ASCII-safe fallbacks can be introduced per-glyph when needed.
// ---------------------------------------------------------------------------

var (
	GlyphDot     = "●" // presence/status indicator
	GlyphCursor  = "▌" // list cursor
	GlyphChevron = "›" // breadcrumb separator
	GlyphCheck   = "✓"
	GlyphCross   = "✗"
	GlyphDiamond = "◆" // approval pending
	GlyphAt      = "@" // mention
	GlyphBullet  = "•"
	GlyphBusy    = "◐" // static activity indicator (reduced motion)
	GlyphBranch  = ""  // nerd-font git branch
	GlyphSpark   = ""  // agent activity
	GlyphLogoBG  = "█" // logo block character
)
