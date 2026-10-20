// Package theme defines the single source of truth for DHI's visual identity.
//
// Every color, spacing value and glyph used anywhere in the TUI must come from
// here. A lint test (see theme_lint_test.go) fails the build if raw colors
// appear outside this package, keeping branding consistent across all
// components and surfaces.
package theme

import (
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// Tokens is the complete design-token set for one DHI theme.
type Tokens struct {
	// Name identifies the theme (shown in doctor output, config, etc).
	Name string

	// Palette.
	Bg          color.Color // application background
	BgPanel     color.Color // panel/card background
	BgElevated  color.Color // overlays, modals, popups
	BgInset     color.Color // sub-columns inside a panel: board columns, chat rail, thread pane
	BgChrome    color.Color // bottom chrome: hint bars, keymap rows (F-025)
	BgSelection color.Color // selected rows, highlighted ranges
	// F-064 shade ladder: header strips (lane/section heads, tab strips)
	// sit one step above the panel; zebra rows and the editor/terminal
	// cursor line are quiet steps that never compete with selection.
	BgHeader     color.Color
	BgRowAlt     color.Color
	BgCursorLine color.Color
	BgInput      color.Color // input fields: a well inside the panel (F-096)
	BgCard       color.Color // cards: one quiet step off the panel (F-096)
	BgAdd        color.Color // diff added-line wash (F-026 P6)
	BgDel        color.Color // diff deleted-line wash (F-026 P6)
	// BgAddStrong / BgDelStrong mark the words that changed inside a
	// changed line (F-049): a deeper step of the same hue as the wash.
	BgAddStrong   color.Color
	BgDelStrong   color.Color
	Border        color.Color // unfocused borders, dividers
	Rule          color.Color // thin in-panel separators, quieter than Border (F-064)
	BorderFocused color.Color // focused element borders
	Text          color.Color // primary text
	TextDim       color.Color // secondary text
	TextMuted     color.Color // hints, disabled elements
	Accent        color.Color // brand primary (saffron, ADR-0041)
	AccentDim     color.Color // quieter primary for secondary emphasis
	Accent2       color.Color // brand secondary (indigo)
	Accent3       color.Color // teal: file kinds, author colors, chips (F-064)
	Accent4       color.Color // rose: file kinds, author colors, chips (F-064)
	Info          color.Color // neutral-blue semantic (informational rows)
	Success       color.Color
	Warning       color.Color
	Danger        color.Color

	// Layout metrics (in terminal cells).
	PadX        int // horizontal padding inside panels
	HeightTab   int // tab bar height
	HeightState int // statusline height
}

// Dark is the default DHI theme, "dhi-dusk" (ADR-0041): a saffron lamp
// on indigo ink. Accent is the brand primary (saffron), Accent2 the
// secondary (indigo), Accent3/Accent4 the tertiaries (teal, lotus).
// Semantic colors keep their conventional hues, held apart from the
// brand hues (TestBrandAndSemanticsAreDistinct).
func Dark() Tokens {
	c := lipgloss.Color
	return Tokens{
		Name: "dhi-dusk",

		Bg:           c("#0D0E16"),
		BgPanel:      c("#11131D"),
		BgElevated:   c("#171A27"),
		BgInset:      c("#08090F"),
		BgChrome:     c("#1D2030"),
		BgSelection:  c("#232842"),
		BgHeader:     c("#181B29"),
		BgRowAlt:     c("#141622"),
		BgCursorLine: c("#191C2B"),
		BgInput:      c("#1A1D2C"),
		BgCard:       c("#151826"),
		BgAdd:        c("#0F2A20"),
		BgDel:        c("#2D1419"),
		BgAddStrong:  c("#164231"),
		BgDelStrong:  c("#4F2229"),
		Border:       c("#272B3D"),
		Rule:         c("#202334"),
		// BorderFocused is a lighter saffron than Accent so a focused
		// edge reads as attention, not brand emphasis.
		BorderFocused: c("#FFB86E"),
		Text:          c("#ECEAF4"),
		TextDim:       c("#A9ACC2"),
		TextMuted:     c("#8D92AB"), // WCAG AA (4.5:1) on every surface incl. the hint bar
		Accent:        c("#FF9F43"),
		AccentDim:     c("#B9773A"),
		Accent2:       c("#A3A4FF"),
		Accent3:       c("#2DD4BF"),
		Accent4:       c("#E879F9"),
		Info:          c("#6CB6FF"),
		Success:       c("#7EE081"),
		Warning:       c("#F5D547"),
		Danger:        c("#FF7B7B"),

		PadX:        1,
		HeightTab:   1,
		HeightState: 1,
	}
}

// Light is "dhi-dawn": warm paper with the dusk hues deepened for
// contrast.
func Light() Tokens {
	c := lipgloss.Color
	return Tokens{
		Name: "dhi-dawn",

		Bg:            c("#F7F4EC"),
		BgPanel:       c("#FCFAF5"),
		BgElevated:    c("#FFFFFF"),
		BgInset:       c("#EFEBE0"),
		BgChrome:      c("#E8E2D3"),
		BgSelection:   c("#E4E3F7"),
		BgHeader:      c("#F2EEE4"),
		BgRowAlt:      c("#F8F5EE"),
		BgCursorLine:  c("#F3EFE6"),
		BgInput:       c("#EEEADF"),
		BgCard:        c("#F9F6EF"),
		BgAdd:         c("#DCEEDC"),
		BgDel:         c("#F7E0DF"),
		BgAddStrong:   c("#B2D8B9"),
		BgDelStrong:   c("#EDBFBF"),
		Border:        c("#D9D3C5"),
		Rule:          c("#E6E0D3"),
		BorderFocused: c("#C25E00"),
		Text:          c("#1E1F2B"),
		TextDim:       c("#4A4D5E"),
		TextMuted:     c("#5D6072"), // WCAG AA on every surface incl. the hint bar
		Accent:        c("#9A4800"),
		AccentDim:     c("#A86A33"),
		Accent2:       c("#4B44C9"),
		Accent3:       c("#0B6B63"),
		Accent4:       c("#A0178F"),
		Info:          c("#1D5BB8"),
		Success:       c("#2F6E12"),
		Warning:       c("#7A5A00"),
		Danger:        c("#B42020"),

		PadX:        1,
		HeightTab:   1,
		HeightState: 1,
	}
}

// HighContrast is the accessibility theme: true-black canvas, white text
// and saturated dusk hues, every text token at or above 6:1 on every
// surface and borders that read against the background.
func HighContrast() Tokens {
	c := lipgloss.Color
	return Tokens{
		Name: "high-contrast",

		Bg:            c("#000000"),
		BgPanel:       c("#000000"),
		BgElevated:    c("#0A0A0A"),
		BgInset:       c("#000000"),
		BgChrome:      c("#1A1A1A"),
		BgSelection:   c("#26264A"),
		BgHeader:      c("#121212"),
		BgRowAlt:      c("#0A0A0A"),
		BgCursorLine:  c("#161616"),
		BgInput:       c("#101010"),
		BgCard:        c("#0A0A0A"),
		BgAdd:         c("#003D1F"),
		BgDel:         c("#4A0F0F"),
		BgAddStrong:   c("#09502B"),
		BgDelStrong:   c("#652121"),
		Border:        c("#8A8FA8"),
		Rule:          c("#4B5060"),
		BorderFocused: c("#FFB000"),
		Text:          c("#FFFFFF"),
		TextDim:       c("#E4E4EE"),
		TextMuted:     c("#C6C8D6"),
		Accent:        c("#FFB000"),
		AccentDim:     c("#E8A050"),
		Accent2:       c("#B8B9FF"),
		Accent3:       c("#5EEAD4"),
		Accent4:       c("#F5A8FF"),
		Info:          c("#8CC8FF"),
		Success:       c("#9CF29E"),
		Warning:       c("#FFE45C"),
		Danger:        c("#FF9494"),

		PadX:        1,
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

// Motion knobs (F-026 P1): one place for animated-effect cadence so
// reduced-motion and tuning never touch component code.
var (
	MotionFrames   = 2                      // view-transition fade frames
	MotionInterval = 100 * time.Millisecond // view-transition tick
)

// Breakpoints are the shared responsive widths in terminal cells.
// kit re-exports them as consts for compatibility (F-026 P1 moved the
// definition here — theme owns the responsive identity).
const (
	WCompact = 60  // below: centered hero / full-width stack
	WDock    = 84  // at/above: docked rail + main pane
	WWide    = 120 // at/above: side-by-side detail floors
)

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

// AddWash / DelWash paint diff line backgrounds (F-026 P6): kind-first
// coloring so added/removed blocks read at a glance.
func AddWash() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgAdd).Foreground(Current.Success)
}

// AddWashStrong / DelWashStrong paint the changed words inside a wash.
func AddWashStrong() lipgloss.Style { return lipgloss.NewStyle().Background(Current.BgAddStrong) }
func DelWashStrong() lipgloss.Style { return lipgloss.NewStyle().Background(Current.BgDelStrong) }

func DelWash() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgDel).Foreground(Current.Danger)
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
		return SurfaceAccent()
	}
	return Current.Border
}

// surface is the active view's id; the shell sets it on every switch.
var surface string

// SetSurface records the active view. Colors no longer depend on it
// (ADR-0041: one identity in every view); it remains so the shell can
// tell which view is live.
func SetSurface(id string) { surface = id }

// SurfaceAccent is the focus color: focused pane edges and rules, the
// active tab and the mode block. It is the same in every view
// (ADR-0041); where you are is told by the tab and the mode label.
func SurfaceAccent() color.Color { return Current.BorderFocused }

// SurfaceText is the focus color as text: the brand primary, which reads
// on selections where the paler focus edge would not.
func SurfaceText() color.Color {
	if Current.Name == HighContrast().Name {
		return Current.BorderFocused
	}
	return Current.Accent
}

// RailActive styles a rail's highlighted row in the focus hue (F-093),
// so the rail, tab and mode block agree on where you are.
func RailActive() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgSelection).Foreground(SurfaceText()).Bold(true)
}

// TabCurrent styles the active view's tab in its identity accent.
func TabCurrent() lipgloss.Style {
	return Pill(SurfaceText()).Bold(true) // the focus hue as text on its tint (ADR-0041)
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

// ModeBlock styles the status row's view block (ADR-0040): the view's
// hue as a solid fill with canvas-colored bold text.
func ModeBlock() lipgloss.Style {
	fill := ModeFill()
	return lipgloss.NewStyle().Background(fill).Foreground(OnFill(fill)).Bold(true)
}

// ModeFill is the focus hue as a solid fill, deepened toward the text
// color until its label reaches AA (a pale hue alone is too faint).
func ModeFill() color.Color { return SolidFill(SurfaceAccent()) }

// SolidFill deepens c toward the text color until a label on it reaches
// AA, so any hue can carry a solid control (buttons, mode block).
func SolidFill(c color.Color) color.Color {
	for i := 0; i < 10 && ratio(OnFill(c), c) < 4.5; i++ {
		c = Blend(c, Current.Text, 0.12)
	}
	return c
}

// OnFill picks the text color for a solid fill: the canvas or the body
// text, whichever reads better on it.
func OnFill(fill color.Color) color.Color {
	if ratio(Current.Bg, fill) >= ratio(Current.Text, fill) {
		return Current.Bg
	}
	return Current.Text
}

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

// ratio is the WCAG contrast ratio between a and b.
func ratio(a, b color.Color) float64 {
	la, lb := relLum(a), relLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ButtonKey styles a pane button's key cap: the focus hue as a solid
// fill, so actions read as pressable (F-093).
func ButtonKey() lipgloss.Style {
	fill := ModeFill()
	return lipgloss.NewStyle().Background(fill).Foreground(OnFill(fill)).Bold(true)
}

// AttentionPill styles the status row's "◆ N waiting" segment.
func AttentionPill() lipgloss.Style { return Pill(Current.Warning).Bold(true) }

// Brand styles the DHI wordmark.
func Brand() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Accent).Bold(true)
}

// Hint styles keyboard hint fragments ("1-9 switch").
func Hint() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.TextMuted) }

// Keycap styles a single key fragment as a raised pill — distinct from
// plain Hint so keys are findable at a glance (F-026 P7 statusline/help).
func Keycap() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgElevated).Foreground(Current.Text)
}

// HelpOverlay styles the help modal box.
func HelpOverlay() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(Current.Accent2).
		Background(Current.BgElevated).
		Padding(1, 2)
}

// Success / Warning / Danger / Info semantic text styles.
func SuccessText() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.Success) }
func WarningText() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.Warning) }
func DangerText() lipgloss.Style  { return lipgloss.NewStyle().Foreground(Current.Danger) }
func InfoText() lipgloss.Style    { return lipgloss.NewStyle().Foreground(Current.Info) }

// AccentDimText styles secondary emphasis: badge counts, quiet accents.
func AccentDimText() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.AccentDim) }

// AccentText / Accent2Bold style primary token emphasis (syntax
// functions on the accent, keywords bold indigo — F-026 P4).
func AccentText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Accent).Bold(true)
}

func Accent2Bold() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Accent2).Bold(true)
}

// TextDim styles secondary text.
func TextDim() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.TextDim) }

// TextStyle styles primary text (the body fg; F-026 P1e transcripts).
func TextStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.Text) }

// TextMuted styles hints, disabled and placeholder text.
func TextMuted() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.TextMuted) }

// DialogEdge / DialogTitle style modal boxes (indigo edge = dialog
// identity, distinct from focused panes).
func DialogEdge() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Accent2)
}

func DialogTitle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Text).Bold(true)
}

// HeaderBg paints header strips: lane and section heads, tab strips,
// dialog title rows (F-064) — one shade above the panel.
func HeaderBg() lipgloss.Style { return lipgloss.NewStyle().Background(Current.BgHeader) }

// SelectionBg paints a selected row's background.
func SelectionBg() lipgloss.Style { return lipgloss.NewStyle().Background(Current.BgSelection) }

// RowAltBg paints every other row of a long list (quiet zebra).
func RowAltBg() lipgloss.Style { return lipgloss.NewStyle().Background(Current.BgRowAlt) }

// CursorLineBg paints the editor/terminal line under the cursor.
func CursorLineBg() lipgloss.Style { return lipgloss.NewStyle().Background(Current.BgCursorLine) }

// RuleText styles thin in-panel separators.
func RuleText() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.Rule) }

// Rule renders a w-cell horizontal separator.
func Rule(w int) string {
	if w <= 0 {
		return ""
	}
	return RuleText().Render(strings.Repeat("─", w))
}

// DeckDivider styles a deck pane's left divider (ADR-0040): the view's
// hue when the pane has focus, the quiet border otherwise, on the
// panel shade.
func DeckDivider(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(panelEdge(focused)).Background(Current.BgPanel)
}

// deckRuleSteps bounds the gradient's color changes: enough to read as a
// fade, few enough to keep rows short.
const deckRuleSteps = 12

// DeckRule renders an n-cell pane header rule (ADR-0040). Focused, it is
// a heavy line fading from the focus hue into the panel shade, so the
// title end glows and the far end dissolves; unfocused, a thin quiet
// rule. High contrast keeps a solid focus line.
func DeckRule(n int, focused bool) string {
	if n <= 0 {
		return ""
	}
	if !focused {
		return lipgloss.NewStyle().Foreground(Current.Rule).Background(Current.BgPanel).
			Render(strings.Repeat("─", n))
	}
	from := SurfaceAccent()
	if Current.Name == HighContrast().Name {
		return lipgloss.NewStyle().Foreground(from).Background(Current.BgPanel).
			Render(strings.Repeat("━", n))
	}
	to := Blend(Current.BgPanel, from, 0.18)
	steps := deckRuleSteps
	if n < steps {
		steps = n
	}
	var b strings.Builder
	done := 0
	for i := 0; i < steps; i++ {
		end := n * (i + 1) / steps
		t := 0.0
		if steps > 1 {
			t = float64(i) / float64(steps-1)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(Blend(from, to, t)).Background(Current.BgPanel).
			Render(strings.Repeat("━", end-done)))
		done = end
	}
	return b.String()
}

// SectionHeader styles an uppercase group label (CHANNELS, Appearance):
// muted and bold so it organises without shouting.
func SectionHeader() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.TextMuted).Bold(true)
}

// pillTint is how far a pill's background leans from the panel toward
// its foreground hue.
const pillTint = 0.16

// Pill styles a small status chip in fg on a tint of fg (F-064): the
// chip reads as the same family as its text without a hard block.
func Pill(fg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(PillBg(fg))
}

// PillBg is the tinted background Pill uses for fg.
func PillBg(fg color.Color) color.Color { return Blend(Current.BgPanel, fg, pillTint) }

// AuthorColor gives a stable, distinct color per author name (chat
// authors, assignees), drawn from the accent ramp.
func AuthorColor(name string) color.Color {
	ramp := []color.Color{Current.Accent, Current.Accent2, Current.Accent3,
		Current.Accent4, Current.Info, Current.Success, Current.Warning}
	h := uint32(2166136261)
	for i := 0; i < len(name); i++ {
		h = (h ^ uint32(name[i])) * 16777619
	}
	return ramp[h%uint32(len(ramp))]
}

// FileKind is the glyph and color a file tree shows for a file name.
// Plain Unicode only (no Nerd Font): the glyph shape is shared and the
// color tells kinds apart.
func FileKind(name string) (glyph string, c color.Color) {
	lower := strings.ToLower(name)
	ext := ""
	if i := strings.LastIndexByte(lower, '.'); i > 0 {
		ext = lower[i+1:]
	}
	switch {
	case lower == "dockerfile" || lower == "makefile" || ext == "sh" || ext == "bash" || ext == "zsh":
		return GlyphFileExec, Current.Success
	case ext == "go" || ext == "mod" || ext == "sum":
		return GlyphFile, Current.Accent
	case ext == "md" || ext == "txt" || ext == "rst":
		return GlyphFileDoc, Current.Info
	case ext == "json" || ext == "yaml" || ext == "yml" || ext == "toml" || ext == "ini" || ext == "env":
		return GlyphFileConf, Current.Warning
	case ext == "ts" || ext == "tsx" || ext == "js" || ext == "jsx" || ext == "mjs":
		return GlyphFile, Current.Accent4
	case ext == "py" || ext == "rb" || ext == "rs" || ext == "java" || ext == "kt" || ext == "c" || ext == "h" || ext == "cpp" || ext == "zig" || ext == "swift":
		return GlyphFile, Current.Accent2
	case ext == "css" || ext == "scss" || ext == "html" || ext == "svg":
		return GlyphFile, Current.Accent3
	}
	return GlyphFile, Current.TextMuted
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
// Component roles (ADR-0041, F-096). Each kind of control owns its colors
// and reads them here, so a control looks the same in every view.
// ---------------------------------------------------------------------------

// ButtonVariant picks a button's role.
type ButtonVariant int

const (
	ButtonPrimary   ButtonVariant = iota // the one action a view suggests: saffron fill
	ButtonSecondary                      // other actions: indigo fill
	ButtonDanger                         // destructive actions: red fill
	ButtonGhost                          // low-emphasis actions: text on the input well
)

// ButtonFill is the solid fill of a variant (ghost buttons sit on the
// input well).
func ButtonFill(v ButtonVariant) color.Color {
	switch v {
	case ButtonSecondary:
		return SolidFill(Current.Accent2)
	case ButtonDanger:
		return SolidFill(Current.Danger)
	case ButtonGhost:
		return Current.BgInput
	}
	return SolidFill(Current.Accent)
}

// Button styles a button face. Focused buttons are bold and, for ghost
// buttons, take the focus color; disabled buttons drop to muted text on
// the input well whatever their variant.
func Button(v ButtonVariant, focused, disabled bool) lipgloss.Style {
	if disabled {
		return lipgloss.NewStyle().Background(Current.BgInput).Foreground(Current.TextMuted)
	}
	fill := ButtonFill(v)
	st := lipgloss.NewStyle().Background(fill).Bold(focused)
	if v == ButtonGhost {
		fg := Current.TextDim
		if focused {
			fg = SurfaceText()
		}
		return st.Foreground(fg)
	}
	return st.Foreground(OnFill(fill))
}

// ButtonKeyOn styles the key shown inside a button face: the face's own
// colors, underlined so the key reads as the shortcut.
func ButtonKeyOn(v ButtonVariant, focused, disabled bool) lipgloss.Style {
	return Button(v, focused, disabled).Underline(!disabled).Bold(true)
}

// Input styles an input field's text on its well.
func Input() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgInput).Foreground(Current.Text)
}

// Placeholder styles an empty input's hint on the well.
func Placeholder() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgInput).Foreground(Current.TextMuted)
}

// InputEdge styles an input's left edge: the focus color when focused,
// the quiet border otherwise, on the well.
func InputEdge(focused bool) lipgloss.Style {
	fg := Current.Border
	if focused {
		fg = Current.BorderFocused
	}
	return lipgloss.NewStyle().Foreground(fg).Background(Current.BgInput)
}

// CardBg paints a card body.
func CardBg() lipgloss.Style { return lipgloss.NewStyle().Background(Current.BgCard) }

// CardStripe styles a card's left stripe: the focus color when the card
// is selected, the secondary otherwise.
func CardStripe(selected bool) lipgloss.Style {
	fg := Current.Accent2
	if selected {
		fg = Current.BorderFocused
	}
	return lipgloss.NewStyle().Foreground(fg).Background(Current.BgCard)
}

// CardTitle styles a card's title row.
func CardTitle(selected bool) lipgloss.Style {
	st := lipgloss.NewStyle().Background(Current.BgCard).Foreground(Current.Text).Bold(true)
	if selected {
		st = st.Foreground(SurfaceText())
	}
	return st
}

// CardText styles a card's body text.
func CardText() lipgloss.Style {
	return lipgloss.NewStyle().Background(Current.BgCard).Foreground(Current.TextDim)
}

// BadgeKind is a status badge's meaning.
type BadgeKind int

const (
	BadgeNeutral BadgeKind = iota
	BadgeOK
	BadgeWarn
	BadgeError
	BadgeInfo
	BadgeBusy
)

// BadgeColor is a badge kind's hue.
func BadgeColor(k BadgeKind) color.Color {
	switch k {
	case BadgeOK:
		return Current.Success
	case BadgeWarn:
		return Current.Warning
	case BadgeError:
		return Current.Danger
	case BadgeInfo:
		return Current.Info
	case BadgeBusy:
		return Current.Accent2
	}
	return Current.TextDim
}

// Badge styles a status badge: its hue on a tint of it.
func Badge(k BadgeKind) lipgloss.Style { return Pill(BadgeColor(k)) }

// Link styles navigable text (file paths, mentions, "open" actions).
func Link() lipgloss.Style { return lipgloss.NewStyle().Foreground(Current.Accent2) }

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
	GlyphBusy    = "●" // static activity indicator (reduced motion); ◐ is missing from common fonts (F-093)
	GlyphTodo    = "○" // a plan step not started (F-067)
	// SparkBars are the eight levels of a sparkline, low to high (F-071).
	SparkBars      = "▁▂▃▄▅▆▇█"
	GlyphBranch    = "⎇" // git branch marker; missing from many fonts, so branches show as a TextDim pill instead (F-064)
	GlyphGutterBar = "▎" // git gutter: added/modified line (F-040)
	GlyphGutterDel = "▁" // git gutter: lines deleted below
	GlyphFile      = "◦" // tree: a source file (colored by FileKind)
	GlyphFileDoc   = "≡" // tree: prose (markdown, text)
	GlyphFileConf  = "∷" // tree: config (json/yaml/toml)
	GlyphFileExec  = "$" // tree: scripts and build files
	GlyphDirOpen   = "▾" // tree: expanded folder
	GlyphDirClosed = "▸" // tree: collapsed folder
	GlyphDivider   = "▏" // deck pane left divider (ADR-0040)
	GlyphSpark     = "◇" // agent activity / transcript agent author (F-026 P1e); Geometric Shapes, not Dingbats, so common terminal fonts have it
)
