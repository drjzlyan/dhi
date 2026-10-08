package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func testPalette() *Palette {
	return NewPalette([]PaletteItem{
		{Group: "Go to", Title: "Editor", Hint: "2", Data: "editor"},
		{Group: "Go to", Title: "Workspace", Hint: "1", Data: "workspace"},
		{Group: "Editor", Title: "Run tests", Hint: ":test", Data: "test"},
		{Title: "Toggle help", Hint: "?", Data: "help"},
	})
}

func typeInto(p *Palette, s string) {
	for _, r := range s {
		p.HandleKey(string(r))
	}
}

func TestPaletteFiltersAndRanks(t *testing.T) {
	p := testPalette()
	if got := len(p.Matches()); got != 4 {
		t.Fatalf("empty query keeps all, got %d", got)
	}
	typeInto(p, "tst")
	m := p.Matches()
	if len(m) == 0 || m[0].Data != "test" {
		t.Fatalf("fuzzy 'tst' should rank 'Run tests' first, got %+v", m)
	}
	typeInto(p, "zzzz")
	if len(p.Matches()) != 0 {
		t.Fatalf("nonsense query should match nothing, got %+v", p.Matches())
	}
	for range "tstzzzz" {
		p.HandleKey("backspace")
	}
	if len(p.Matches()) != 4 || p.Query() != "" {
		t.Fatal("backspacing to empty restores the full list")
	}
}

func TestPalettePickAndClose(t *testing.T) {
	p := testPalette()
	p.HandleKey("down")
	picked, closed := p.HandleKey("enter")
	if !closed || picked == nil || picked.Data != "workspace" {
		t.Fatalf("picked %+v closed=%v", picked, closed)
	}
	p = testPalette()
	if picked, closed := p.HandleKey("esc"); picked != nil || !closed {
		t.Fatal("esc closes without picking")
	}
	p = testPalette()
	typeInto(p, "zzzz")
	if picked, closed := p.HandleKey("enter"); picked != nil || closed {
		t.Fatal("enter with no matches does nothing")
	}
}

func TestPaletteCursorClampsAndFollowsFilter(t *testing.T) {
	p := testPalette()
	for i := 0; i < 10; i++ {
		p.HandleKey("down")
	}
	if picked, _ := p.HandleKey("enter"); picked.Data != "help" {
		t.Fatalf("cursor should clamp at the last item, got %v", picked.Data)
	}
	p = testPalette()
	for i := 0; i < 3; i++ {
		p.HandleKey("down")
	}
	typeInto(p, "wor") // narrowing resets the cursor to the best match
	if picked, _ := p.HandleKey("enter"); picked == nil || picked.Data != "workspace" {
		t.Fatalf("picked %+v", picked)
	}
}

func TestPaletteViewShowsRowsHintsAndSelection(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	p := testPalette()
	out := ansi.Strip(p.View(60))
	for _, want := range []string{"command palette", "› ", "Go to Editor", "Run tests", ":test", "4 commands", "esc close"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	typeInto(p, "zzzz")
	if out := ansi.Strip(p.View(60)); !strings.Contains(out, "no matching command") {
		t.Fatalf("empty state missing:\n%s", out)
	}
	// Width is honored (including the minimum).
	for _, w := range []int{20, 60, 100} {
		for _, l := range strings.Split(ansi.Strip(testPalette().View(w)), "\n") {
			if got := len([]rune(l)); got > max(w, 40) {
				t.Fatalf("width %d: line of %d cells: %q", w, got, l)
			}
		}
	}
}

func TestRailRowAtMapsClicksToRows(t *testing.T) {
	r := &Rail{Rows: make([]RailRow, 4), Active: 0, Width: 20, Height: 10, Foot: "x"}
	for y, want := range []int{0, 1, 2, 3} {
		if got, ok := r.RowAt(y); !ok || got != want {
			t.Errorf("y=%d → %d,%v want %d", y, got, ok, want)
		}
	}
	for _, y := range []int{4, 8, 9, 20, -1} { // blank fill, foot, outside
		if _, ok := r.RowAt(y); ok {
			t.Errorf("y=%d should hit no row", y)
		}
	}
	// A title row shifts everything down by one and is itself not a row.
	r = &Rail{Title: "x", Rows: make([]RailRow, 3), Width: 20, Height: 8}
	if _, ok := r.RowAt(0); ok {
		t.Error("the title is not clickable")
	}
	if i, ok := r.RowAt(1); !ok || i != 0 {
		t.Errorf("first row under a title: %d,%v", i, ok)
	}
	// An overflowing rail honors its scroll window.
	r = &Rail{Rows: make([]RailRow, 10), Active: 9, Width: 20, Height: 5, Foot: "x"}
	if i, ok := r.RowAt(0); !ok || i != 6 {
		t.Errorf("scrolled rail: top row = %d,%v want 6", i, ok)
	}
}
