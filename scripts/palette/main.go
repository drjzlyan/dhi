// Command palette prints DHI's brand palette and component roles for one
// theme as ANSI, for reviewing a theme change (ADR-0041):
//
//	go run ./scripts/palette [dhi-dusk|dhi-dawn|high-contrast]
//
// Pipe it through charmbracelet/freeze to get a PNG.
package main

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func main() {
	name := "dhi-dusk"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	switch name {
	case theme.Light().Name:
		theme.Current = theme.Light()
	case theme.HighContrast().Name:
		theme.Current = theme.HighContrast()
	default:
		theme.Current = theme.Dark()
	}
	t := theme.Current
	const w = 96
	bg := lipgloss.NewStyle().Background(t.BgPanel)
	var rows []string
	add := func(s string) { rows = append(rows, kit.PaintRow(s, w, bg)) }
	head := func(s string) {
		add("")
		add(bg.Render(" ") + theme.SectionHeader().Background(t.BgPanel).Render(strings.ToUpper(s)))
	}

	add(bg.Render(" ") + theme.Brand().Background(t.BgPanel).Render("◆ dhi") +
		theme.TextDim().Background(t.BgPanel).Render("  "+t.Name+" — a saffron lamp on indigo ink"))

	head("brand and semantics")
	sw := func(label string, c color.Color) string {
		return lipgloss.NewStyle().Background(c).Foreground(theme.OnFill(c)).Bold(true).Render(" "+label+" ") +
			bg.Render(" ")
	}
	add(bg.Render(" ") + sw("primary", t.Accent) + sw("secondary", t.Accent2) + sw("teal", t.Accent3) +
		sw("lotus", t.Accent4) + sw("info", t.Info))
	add(bg.Render(" ") + sw("success", t.Success) + sw("warning", t.Warning) + sw("danger", t.Danger) +
		sw("focus", t.BorderFocused))

	head("text")
	txt := func(label string, c color.Color) string {
		return lipgloss.NewStyle().Foreground(c).Background(t.BgPanel).Render(label) + bg.Render("   ")
	}
	add(bg.Render(" ") + txt("Text body", t.Text) + txt("Dim secondary", t.TextDim) +
		txt("Muted hint", t.TextMuted) + theme.Link().Background(t.BgPanel).Render("api/main.go"))

	head("buttons")
	btn := func(v theme.ButtonVariant, key, label string, focused, disabled bool) string {
		return theme.ButtonKeyOn(v, focused, disabled).Render(" "+key) +
			theme.Button(v, focused, disabled).Render(" "+label+" ") + bg.Render("  ")
	}
	add(bg.Render(" ") + btn(theme.ButtonPrimary, "⏎", "Continue", true, false) +
		btn(theme.ButtonSecondary, "e", "Edit", false, false) +
		btn(theme.ButtonGhost, "s", "Skip", false, false) +
		btn(theme.ButtonDanger, "d", "Delete", false, false) +
		btn(theme.ButtonPrimary, "i", "Install", false, true))

	head("inputs")
	input := func(label, value string, focused bool) string {
		lab := theme.TextDim().Background(t.BgPanel).Render(fmt.Sprintf(" %-10s", label))
		edge := theme.InputEdge(focused).Render("▎")
		body := theme.Input().Render(" " + value)
		if value == "" {
			body = theme.Placeholder().Render(" you@example.com")
		}
		pad := theme.Input().Render(strings.Repeat(" ", 36-lipgloss.Width(body)))
		return lab + edge + body + pad
	}
	add(input("name", "Ada Lovelace", true))
	add(input("email", "", false))

	head("toggles and badges")
	add(bg.Render(" ") + theme.SuccessText().Background(t.BgPanel).Render("✓") +
		theme.TextStyle().Background(t.BgPanel).Render(" Co-author trailer   ") +
		theme.TextMuted().Background(t.BgPanel).Render("○") +
		theme.TextStyle().Background(t.BgPanel).Render(" Copyright header   ") +
		theme.AccentText().Background(t.BgPanel).Render("●") +
		theme.TextStyle().Background(t.BgPanel).Render(" squad  ") +
		theme.TextMuted().Background(t.BgPanel).Render("○") +
		theme.TextStyle().Background(t.BgPanel).Render(" solo"))
	badge := func(k theme.BadgeKind, s string) string { return theme.Badge(k).Render(" "+s+" ") + bg.Render(" ") }
	add(bg.Render(" ") + badge(theme.BadgeOK, "✓ installed") + badge(theme.BadgeWarn, "! update") +
		badge(theme.BadgeError, "✗ missing") + badge(theme.BadgeInfo, "i optional") +
		badge(theme.BadgeBusy, "● working") + badge(theme.BadgeNeutral, "· later") +
		theme.AttentionPill().Render(" ◆ 2 waiting "))

	head("cards")
	card := func(selected bool, title, body string) {
		cw := 44
		stripe := theme.CardStripe(selected).Render("▌")
		fill := func(s string, st lipgloss.Style) string {
			return st.Render(s + strings.Repeat(" ", max(0, cw-lipgloss.Width(s))))
		}
		add(bg.Render(" ") + stripe + fill(" "+title, theme.CardTitle(selected)))
		add(bg.Render(" ") + stripe + fill(" "+body, theme.CardText()))
	}
	card(true, "Git identity", "Commits are signed with this name and email.")
	add("")
	card(false, "Coding CLI", "claude 2.1 found · codex not installed")

	head("tabs and status")
	tabs := theme.Brand().Render(" ◆ dhi ") + theme.TabCurrent().Render(" 1 Editor ") +
		theme.TabInactive().Render("  2 Workspace  3 Ideator  4 Reviewer  5 Settings")
	add(tabs)
	add(theme.ModeBlock().Render(" EDITOR ") + theme.ChromeStatus(t.TextDim).Render(" buffer ") +
		theme.AttentionPill().Render(" ◆ 1 waiting ") + bg.Render("  ") +
		theme.Keycap().Render(" i ") + theme.Hint().Background(t.BgPanel).Render(" insert  ") +
		theme.Keycap().Render(" ^p ") + theme.Hint().Background(t.BgPanel).Render(" palette"))
	add("")
	fmt.Println(strings.Join(rows, "\n"))
}
