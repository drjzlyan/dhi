package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestListNavigationAndClamping(t *testing.T) {
	l := &List{Width: 30}
	l.SetItems([]Item{{Title: "a"}, {Title: "b"}, {Title: "c"}})

	if _, ok := l.Selected(); !ok {
		t.Fatal("selected should exist")
	}
	if l.Cursor != 0 {
		t.Fatalf("cursor starts at %d, want 0", l.Cursor)
	}
	l.Up()
	if l.Cursor != 0 {
		t.Fatalf("Up at top clamped to %d", l.Cursor)
	}
	l.Down()
	l.Down()
	l.Down()
	if l.Cursor != 2 {
		t.Fatalf("Down clamped to %d, want 2", l.Cursor)
	}
	if !l.HandleKey("g") || l.Cursor != 0 {
		t.Fatal("HandleKey(g) should jump to first")
	}
	if !l.HandleKey("G") || l.Cursor != 2 {
		t.Fatal("HandleKey(G) should jump to last")
	}
	if l.HandleKey("x") {
		t.Fatal("unknown key must not be consumed")
	}
}

func TestListScrollWindow(t *testing.T) {
	items := make([]Item, 10)
	for i := range items {
		items[i] = Item{Title: string(rune('a' + i))}
	}
	l := &List{Width: 20, Height: 3}
	l.SetItems(items)
	l.moveTo(9)
	rows := l.visibleRows()
	if len(rows) != 3 || rows[0].Title != "h" {
		t.Fatalf("scroll window wrong: first=%q len=%d", rows[0].Title, len(rows))
	}
}

func TestTabsClampAndActiveID(t *testing.T) {
	tb := NewTabs([2]string{"home", "Home"}, [2]string{"trees", "Trees"})
	if tb.SetActive(5); tb.Active != 1 {
		t.Fatalf("SetActive clamped to %d", tb.Active)
	}
	if got := tb.ActiveID(); got != "trees" {
		t.Fatalf("ActiveID=%q", got)
	}
	if tb.SetActive(1) {
		t.Fatal("setting same index must report no change")
	}
}

func TestStatusLinePadsToWidth(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	sl := DefaultStatusLine("Home")
	sl.Width = 80
	out := sl.View()
	if w := runeWidth(ansi.Strip(out)); w != 80 {
		t.Fatalf("statusline width %d, want 80", w)
	}
}

func TestListRowsPadExactlyToWidth(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	l := &List{Width: 40}
	l.SetItems([]Item{
		{Title: "payments", Badge: "dirty"},
		{Title: "ledger", Badge: "clean"},
		{Title: "storefront", Badge: "M/R"},
	})
	l.Down()
	for i, row := range strings.Split(l.View(), "\n") {
		if w := len([]rune(ansi.Strip(row))); w != 40 {
			t.Fatalf("row %d width=%d want 40 (%q)", i, w, ansi.Strip(row))
		}
	}
}
