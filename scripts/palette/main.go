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
	bl, _ := kit.Buttons([]kit.Button{{Key: "⏎", Label: "Continue", Focused: true},
		{Key: "e", Label: "Edit", Variant: theme.ButtonSecondary}, {Key: "s", Label: "Skip", Variant: theme.ButtonGhost},
		{Key: "d", Label: "Delete", Variant: theme.ButtonDanger}, {Key: "i", Label: "Install", Disabled: true}}, w-2)
	for _, l := range bl {
		add(bg.Render(" ") + l)
	}

	head("inputs and controls")
	who := kit.NewInput("Ada Lovelace", "")
	who.Focused = true
	email := kit.Input{Placeholder: "you@example.com", Help: "Used as the author of every commit you make."}
	label := func(s string) string { return theme.TextDim().Background(t.BgPanel).Render(fmt.Sprintf(" %-10s", s)) }
	add(label("name") + who.View(40)[0])
	for i, l := range email.View(40) {
		lab := label("")
		if i == 0 {
			lab = label("email")
		}
		add(lab + l)
	}
	add(bg.Render(" ") + kit.Checkbox("Co-author trailer", true, false) + bg.Render("   ") +
		kit.Checkbox("Copyright header", false, false) + bg.Render("   ") + kit.Radio("squad", true, true) +
		bg.Render("  ") + kit.Radio("solo", false, false) + bg.Render("   ") + kit.Toggle("conventional", true))
	add(bg.Render(" ") + kit.Badge(theme.BadgeOK, "installed") + bg.Render(" ") + kit.Badge(theme.BadgeWarn, "update") +
		bg.Render(" ") + kit.Badge(theme.BadgeError, "missing") + bg.Render(" ") + kit.Badge(theme.BadgeInfo, "optional") +
		bg.Render(" ") + kit.Badge(theme.BadgeBusy, "working") + bg.Render(" ") + kit.Badge(theme.BadgeNeutral, "later") +
		bg.Render(" ") + theme.AttentionPill().Render(" ◆ 2 waiting "))

	head("cards and checklist")
	c1, _ := kit.Card{Title: "Git identity", Body: "Commits are signed with this name and email.", Selected: true,
		Buttons: []kit.Button{{Key: "⏎", Label: "Use it", Focused: true}, {Key: "e", Label: "Change", Variant: theme.ButtonGhost}}}.View(46)
	c2, _ := kit.Card{Title: "Coding CLI", Body: "claude 2.1 found · codex not installed", Badge: "ready", BadgeKind: theme.BadgeOK}.View(46)
	for i := 0; i < max(len(c1), len(c2)); i++ {
		l, r := "", ""
		if i < len(c1) {
			l = c1[i]
		} else {
			l = bg.Render(strings.Repeat(" ", 46))
		}
		if i < len(c2) {
			r = c2[i]
		}
		add(bg.Render(" ") + l + bg.Render("  ") + r)
	}
	add("")
	for _, l := range (kit.Checklist{Focus: true, Cursor: 1, Items: []kit.ChecklistItem{
		{Title: "Git identity", Why: "sign your commits", State: kit.ItemDone},
		{Title: "Create a workspace", Why: "group the repos you work on"},
		{Title: "Pick a team", Why: "agents that take tasks"},
		{Title: "Integrations", Why: "connect GitHub", State: kit.ItemSkipped},
	}}).View(70) {
		add(bg.Render(" ") + l)
	}

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
