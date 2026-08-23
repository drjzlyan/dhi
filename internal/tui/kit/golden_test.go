package kit

import (
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
