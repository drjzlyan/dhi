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
	Accent        color.Color // primary brand accent (cyan)
	AccentDim     color.Color // quieter accent for secondary emphasis
	Accent2       color.Color // secondary brand accent (violet)
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

// Dark is the default DHI theme: near-black canvas with a cyan-to-violet
// futuristic accent ramp.
func Dark() Tokens {
	c := lipgloss.Color
	return Tokens{
		Name: "dark-futuristic",

		Bg:           c("#0B0E14"),
		BgPanel:      c("#10141B"),
		BgElevated:   c("#151B26"),
		BgInset:      c("#07090D"),
		BgChrome:     c("#1C2430"),
		BgSelection:  c("#1B2739"),
		BgHeader:     c("#161D29"),
		BgRowAlt:     c("#131821"),
		BgCursorLine: c("#171E2A"),
		BgAdd:        c("#0D2B22"),
		BgDel:        c("#2B1215"),
		BgAddStrong:  c("#124131"),
		BgDelStrong:  c("#4E2225"),
		Border:       c("#232C3B"),
		Rule:         c("#1E2633"),
		// BorderFocused is a brighter cyan than Accent so a focused
		// edge reads as attention, not brand emphasis (F-026 P1).
		BorderFocused: c("#67E8F9"),
		Text:          c("#E6EDF3"),
		TextDim:       c("#9AA7B8"),
		TextMuted:     c("#8391A5"), // WCAG AA (4.5:1) on every surface incl. the hint bar
		Accent:        c("#22D3EE"),
		AccentDim:     c("#1083A3"),
		Accent2:       c("#A78BFA"),
		Accent3:       c("#2DD4BF"),
		Accent4:       c("#F472B6"),
		Info:          c("#60A5FA"),
		Success:       c("#34D399"),
		Warning:       c("#FBBF24"),
		Danger:        c("#F87171"),

		PadX:        1,
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
		BgHeader:      c("#F0ECE1"),
		BgRowAlt:      c("#F6F3EB"),
		BgCursorLine:  c("#F2EEE4"),
		BgAdd:         c("#D9EBDD"),
		BgDel:         c("#F6DFDF"),
		BgAddStrong:   c("#AED3C1"),
		BgDelStrong:   c("#ECBEBE"),
		Border:        c("#D8D2C4"),
		Rule:          c("#E4DED1"),
		BorderFocused: c("#0891B2"),
		Text:          c("#1F2937"),
		TextDim:       c("#4B5563"),
		TextMuted:     c("#5C6472"), // WCAG AA on every surface incl. the hint bar
		Accent:        c("#0D6B84"),
		AccentDim:     c("#4A879A"),
		Accent2:       c("#6D28D9"),
		Accent3:       c("#0B6B63"),
		Accent4:       c("#A3154F"),
		Info:          c("#1D4ED8"),
		Success:       c("#047152"),
		Warning:       c("#A14A08"),
		Danger:        c("#B91C1C"),

		PadX:        1,
		HeightTab:   1,
		HeightState: 1,
	}
}

// HighContrast is the accessibility theme: true-black canvas, white text
// and saturated accents, every text token at or above 6:1 on every
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
		BgSelection:   c("#1F3A5F"),
		BgHeader:      c("#121212"),
		BgRowAlt:      c("#0A0A0A"),
		BgCursorLine:  c("#161616"),
		BgAdd:         c("#003D1F"),
		BgDel:         c("#4A0F0F"),
		BgAddStrong:   c("#09502B"),
		BgDelStrong:   c("#652121"),
		Border:        c("#7C8AA0"),
		Rule:          c("#4B5563"),
		BorderFocused: c("#00E5FF"),
		Text:          c("#FFFFFF"),
		TextDim:       c("#E0E6ED"),
		TextMuted:     c("#C2CAD6"),
		Accent:        c("#00E5FF"),
		AccentDim:     c("#4DD0E1"),
		Accent2:       c("#C4B5FD"),
		Accent3:       c("#5EEAD4"),
		Accent4:       c("#F9A8D4"),
		Info:          c("#7DB7FF"),
		Success:       c("#4ADE80"),
		Warning:       c("#FFD54A"),
		Danger:        c("#FF8A8A"),

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

// SetSurface records the active view for its identity accent (F-055).
func SetSurface(id string) { surface = id }

// SurfaceAccent is the active view's identity color, used on its focused
// panel edges and its tab, so a glance tells where you are: Workspace
// keeps the brand cyan, Editor violet, Ideator amber, Reviewer green,
// Settings neutral. High contrast keeps one focus color for clarity.
func SurfaceAccent() color.Color {
	if Current.Name == HighContrast().Name {
		return Current.BorderFocused
	}
	switch surface {
	case "editor":
		return Current.Accent2
	case "ideator":
		return Current.Warning
	case "reviewer":
		return Current.Success
	case "settings":
		return Current.TextDim
	}
	return Current.BorderFocused
}

// TabCurrent styles the active view's tab in its identity accent.
func TabCurrent() lipgloss.Style {
	return Pill(SurfaceAccent()).Bold(true) // tinted in the view's own hue (F-064)
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
// functions on the accent, keywords bold violet — F-026 P4).
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

// DialogEdge / DialogTitle style modal boxes (violet edge = dialog
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
// Glyphs. Nerd Font glyphs degrade gracefully to boxes on plain fonts;
// ASCII-safe fallbacks can be introduced per-glyph when needed.
// ---------------------------------------------------------------------------

var (
	GlyphDot       = "●" // presence/status indicator
	GlyphCursor    = "▌" // list cursor
	GlyphChevron   = "›" // breadcrumb separator
	GlyphCheck     = "✓"
	GlyphCross     = "✗"
	GlyphDiamond   = "◆" // approval pending
	GlyphAt        = "@" // mention
	GlyphBullet    = "•"
	GlyphBusy      = "◐" // static activity indicator (reduced motion)
	GlyphBranch    = "⎇" // git branch marker; missing from many fonts, so branches show as a TextDim pill instead (F-064)
	GlyphGutterBar = "▎" // git gutter: added/modified line (F-040)
	GlyphGutterDel = "▁" // git gutter: lines deleted below
	GlyphFile      = "◦" // tree: a source file (colored by FileKind)
	GlyphFileDoc   = "≡" // tree: prose (markdown, text)
	GlyphFileConf  = "∷" // tree: config (json/yaml/toml)
	GlyphFileExec  = "$" // tree: scripts and build files
	GlyphDirOpen   = "▾" // tree: expanded folder
	GlyphDirClosed = "▸" // tree: collapsed folder
	GlyphSpark     = "◇" // agent activity / transcript agent author (F-026 P1e); Geometric Shapes, not Dingbats, so common terminal fonts have it
)
