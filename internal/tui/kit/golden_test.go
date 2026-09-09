package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Golden snapshots for visual primitives. Regenerate after intentional visual
// changes: DHI_UPDATE_GOLDENS=1 go test ./internal/tui/...
func TestGoldenPanel(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	p := NewPanel("Worktrees", true).
		SetContent("  main              clean",
			"  feature/login     dirty",
			"  review/pr-42      clean")
	golden.Snapshot(t, "panel_focused", p.View())

	p.Focused = false
	golden.Snapshot(t, "panel_unfocused", p.View())
}

func TestGoldenTabsAndStatus(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	tb := NewTabs(
		[2]string{"home", "Home"}, [2]string{"editor", "Editor"}, [2]string{"trees", "Trees"})
	tb.Active = 1
	tb.Width = 50
	golden.Snapshot(t, "tabs_active_editor", tb.View())
}

func TestGoldenListWithBadges(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	l := &List{Width: 40}
	l.SetItems([]Item{
		{Title: "payments", Badge: "dirty"},
		{Title: "ledger", Badge: "clean"},
		{Title: "storefront", Badge: "M/R"},
	})
	l.Down()
	golden.Snapshot(t, "list_badges", l.View())
}

func TestGoldenModalOverBackdrop(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	backdrop := []string{
		"▌ INBOX            !2",
		"  BOARD             3",
		"  CHANNELS          ●",
		"  REPOS             2",
		"",
		"  main pane content row",
	}
	m := &Modal{Title: "remove team", Lines: []string{
		"delete team platform and its channel?",
		"agents keep their manifests.",
	}}
	out := Overlay(backdrop, m.View(), 60, 12)
	golden.Snapshot(t, "modal_over_backdrop", out)
}

func TestGoldenFormFields(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	f := NewForm("new team",
		NewTextField("name", "platform"),
		NewToggleField("lead", []string{"you", "scout", "fixer"}, 0),
		NewTextField("members", "scout, fixer"))
	f.HandleKey("tab")
	var b strings.Builder
	for _, l := range f.View() {
		b.WriteString(l)
		b.WriteString("\n")
	}
	golden.Snapshot(t, "form_fields", strings.TrimSuffix(b.String(), "\n"))
}

func TestGoldenColumnsLanes(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	c := &Columns{Width: 78, Height: 5, Cols: []Column{
		{Title: "backlog", Rows: []string{"fix-login", "spike-cache", "docs"}},
		{Title: "active", Rows: []string{"auth-flow"}},
		{Title: "in-review"},
		{Title: "done", Rows: []string{"scaffold"}},
	}}
	c.Cols[1].Cursor = 0
	golden.Snapshot(t, "columns_lanes", c.View())
}
