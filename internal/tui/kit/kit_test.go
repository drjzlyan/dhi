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

// TestPanelRowsAllEqualWidth is the corner-cut regression (F-024): the
// titled top edge must be exactly as wide as the body and bottom rows.
func TestPanelRowsAllEqualWidth(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	for _, title := range []string{"Worktrees", "channels", ""} {
		p := NewPanel(title, true).SetContent("row one", "row two")
		rows := strings.Split(p.View(), "\n")
		want := runeWidth(ansi.Strip(rows[len(rows)-1])) // bottom edge is the truth
		if want == 0 {
			t.Fatal("panel rendered empty")
		}
		for i, row := range rows {
			if w := runeWidth(ansi.Strip(row)); w != want {
				t.Fatalf("panel %q row %d width=%d, want %d (%q)",
					title, i, w, want, ansi.Strip(row))
			}
		}
	}
}

func TestModalSizesAndOverlay(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	backdrop := []string{"line one", "line two", "line three", "line four"}
	m := &Modal{Title: "confirm", Lines: []string{"remove team-a?"}, Error: "bad value"}
	out := Overlay(backdrop, m.View(), 60, 9)
	rows := strings.Split(out, "\n")
	if len(rows) != 9 {
		t.Fatalf("overlay rows=%d, want 9", len(rows))
	}
	for i, r := range rows {
		if w := runeWidth(ansi.Strip(r)); w != 60 {
			t.Fatalf("row %d width=%d, want 60", i, w)
		}
	}
	if !strings.Contains(ansi.Strip(out), "remove team-a?") {
		t.Fatal("modal body missing from overlay")
	}
	if !strings.Contains(ansi.Strip(out), "✗ bad value") {
		t.Fatal("modal error row missing")
	}
	if !strings.Contains(ansi.Strip(out), "line one") ||
		!strings.Contains(ansi.Strip(out), "line two") {
		t.Fatal("backdrop lines lost")
	}
}

func TestModalClipsStyledRowsToWidth(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := &Modal{Title: "t", Width: 12, Lines: []string{"a-very-long-line-here"}}
	for i, row := range strings.Split(m.View(), "\n") {
		if w := runeWidth(ansi.Strip(row)); w != 12 {
			t.Fatalf("row %d width=%d, want 12", i, w)
		}
	}
}

func TestFormContract(t *testing.T) {
	f := NewForm("new agent",
		NewTextField("id", "scout"),
		NewToggleField("runtime", []string{"claude", "codex"}, 0))

	if f.HandleKey("x"); f.Values()[0] != "scoutx" {
		t.Fatalf("value=%q", f.Values()[0])
	}
	f.HandleKey("backspace")
	if f.Values()[0] != "scout" {
		t.Fatalf("backspace: %q", f.Values()[0])
	}
	f.HandleKey("tab")
	f.HandleKey("z")
	if f.Values()[1] != "claude" {
		t.Fatalf("toggle mutated by printable: %q", f.Values()[1])
	}
	f.HandleKey("right")
	if f.Values()[1] != "codex" {
		t.Fatalf("right cycle: %q", f.Values()[1])
	}
	f.HandleKey("left")
	if f.Values()[1] != "claude" {
		t.Fatalf("left cycle: %q", f.Values()[1])
	}
	if got := f.HandleKey("enter"); got != FormSubmit {
		t.Fatal("enter must submit")
	}
	if got := f.HandleKey("esc"); got != FormCancel {
		t.Fatal("esc must cancel")
	}
}

func TestFormBusySwallowsAll(t *testing.T) {
	f := NewForm("t", NewTextField("id", "a"))
	f.Busy = true
	for _, k := range []string{"esc", "enter", "tab", "x", "backspace"} {
		if got := f.HandleKey(k); got != FormNone {
			t.Fatalf("busy leaked %q", k)
		}
	}
	if f.Values()[0] != "a" {
		t.Fatalf("busy mutated value: %q", f.Values()[0])
	}
}

func TestColumnsNavAndClamp(t *testing.T) {
	c := &Columns{Width: 40, Cols: []Column{
		{Title: "todo", Rows: []string{"a", "b"}},
		{Title: "doing", Rows: []string{"c"}},
		{Title: "done"},
	}}
	if !c.HandleKey("l") || c.Active != 1 {
		t.Fatal("l must move right")
	}
	c.HandleKey("l")
	c.HandleKey("l")
	if c.Active != 2 {
		t.Fatalf("active=%d, want clamped 2", c.Active)
	}
	c.HandleKey("h")
	c.HandleKey("h")
	c.HandleKey("h")
	if c.Active != 0 {
		t.Fatalf("active=%d, want clamped 0", c.Active)
	}
	c.HandleKey("j")
	c.HandleKey("j")
	if _, cur := c.Selected(); cur != 1 {
		t.Fatalf("cursor=%d, want clamped 1", cur)
	}
	if c.HandleKey("x") {
		t.Fatal("unknown key must not be consumed")
	}
}

func TestColumnsViewRowsPadded(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	c := &Columns{Width: 40, Height: 2, Cols: []Column{
		{Title: "todo", Rows: []string{"a", "b"}},
		{Title: "doing"},
	}}
	rows := strings.Split(c.View(), "\n")
	if len(rows) != 3 {
		t.Fatalf("rows=%d, want header+2", len(rows))
	}
	for i, r := range rows {
		if w := runeWidth(ansi.Strip(r)); w != 40 {
			t.Fatalf("row %d width=%d, want 40 (%q)", i, w, ansi.Strip(r))
		}
	}
	if !strings.Contains(ansi.Strip(rows[1]), "—") {
		t.Fatal("empty lane must show the placeholder")
	}
}

func TestHintBarChromeRow(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	out := ansi.Strip(HintBar(50, "", "n new", "e edit", "x delete"))
	if !strings.Contains(out, "n new") || !strings.Contains(out, "e edit") {
		t.Fatalf("hints missing: %q", out)
	}
	if w := runeWidth(ansi.Strip(out)); w != 50 {
		t.Fatalf("width=%d, want 50", w)
	}
	// The chrome background is structural (theme token + style); color
	// SGR assertions depend on the test terminal's color profile, so
	// only geometry + content are pinned here.
	// A status segment renders before the hints.
	withStatus := ansi.Strip(HintBar(50,
		theme.ChromeStatus(theme.Current.Success).Render("✓ saved"),
		"enter confirm"))
	if !strings.Contains(withStatus, "✓ saved") || !strings.Contains(withStatus, "enter confirm") {
		t.Fatalf("status row wrong: %q", withStatus)
	}
	// Long hint lists clip to width without breaking the row.
	if w := runeWidth(ansi.Strip(HintBar(10, "", "aaaaaaaaaaaaaaaaaaaa"))); w != 10 {
		t.Fatal("unclipped hint bar")
	}
}

func TestRailView(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	r := &Rail{
		Title:  "workspace",
		Active: 1,
		Width:  24,
		Height: 6,
		Rows: []RailRow{
			{Label: "INBOX", Count: "4"},
			{Label: "BOARD", Count: "2"},
			{Label: "CHANNELS"},
			{Label: "REPOS"},
		},
		Foot: "[ ] sections",
	}
	out := ansi.Strip(r.View())
	for _, want := range []string{"WORKSPACE", "INBOX", "BOARD", "CHANNELS", "REPOS", "[ ] sections"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rail missing %q:\n%s", want, out)
		}
	}
	rows := strings.Split(out, "\n")
	if len(rows) != 6 {
		t.Fatalf("rows=%d, want 6", len(rows))
	}
	for i, row := range rows {
		if w := runeWidth(row); w != 24 {
			t.Fatalf("row %d width=%d, want 24 (%q)", i, w, row)
		}
	}
	// Active row carries the cursor marker.
	if !strings.Contains(rows[2], "▌") {
		t.Fatalf("active marker missing: %q", rows[2])
	}
	// No title, no foot: content height.
	r2 := &Rail{Rows: []RailRow{{Label: "a"}, {Label: "b"}}, Width: 10}
	if got := len(strings.Split(r2.View(), "\n")); got != 2 {
		t.Fatalf("bare rail rows=%d, want 2", got)
	}
}

func TestFormAcceptsSpace(t *testing.T) {
	f := NewForm("t", NewTextField("name", ""))
	for _, k := range []string{"A", "d", "a", " ", "L"} {
		f.HandleKey(k)
	}
	if got := f.Values()[0]; got != "Ada L" {
		t.Fatalf("value = %q", got)
	}
}
