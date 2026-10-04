package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestFormTextCursor(t *testing.T) {
	f := NewForm("edit", NewTextField("name", "abc"))
	if f.Cur() != 0 {
		t.Fatalf("Cur = %d", f.Cur())
	}
	// Cursor starts at the end of the pre-fill: a rune appends.
	f.HandleKey("d")
	if got := f.Values()[0]; got != "abcd" {
		t.Fatalf("append = %q", got)
	}
	// left, left, insert mid-value.
	f.HandleKey("left")
	f.HandleKey("left")
	f.HandleKey("X")
	if got := f.Values()[0]; got != "abXcd" {
		t.Fatalf("mid insert = %q", got)
	}
	// backspace deletes before the cursor.
	f.HandleKey("backspace")
	if got := f.Values()[0]; got != "abcd" {
		t.Fatalf("backspace = %q", got)
	}
	// home + rune inserts at the head.
	f.HandleKey("home")
	f.HandleKey("H")
	if got := f.Values()[0]; got != "Habcd" {
		t.Fatalf("home insert = %q", got)
	}
}

func TestFormShiftTabAndPaste(t *testing.T) {
	f := NewForm("edit", NewTextField("a", ""), NewTextField("b", ""))
	f.HandleKey("shift+tab")
	if f.Cur() != 1 {
		t.Fatalf("shift+tab Cur = %d", f.Cur())
	}
	f.HandleKey("paste:hello world")
	if got := f.Values()[1]; got != "hello world" {
		t.Fatalf("paste = %q", got)
	}
	// Control runes inside a paste are dropped.
	f.HandleKey("paste:x\x08y")
	if got := f.Values()[1]; got != "hello worldxy" {
		t.Fatalf("paste filtered = %q", got)
	}
}

func TestFormDeleteKey(t *testing.T) {
	f := NewForm("edit", NewTextField("a", "ab"))
	f.HandleKey("home")
	f.HandleKey("delete")
	if got := f.Values()[0]; got != "b" {
		t.Fatalf("delete = %q", got)
	}
}

func TestListGroupRowsSkipped(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	l := &List{Width: 20}
	l.SetItems([]Item{
		{Title: "open", Group: true},
		{Title: "first"},
		{Title: "second", Group: true},
		{Title: "third"},
	})
	if l.Cursor != 1 {
		t.Fatalf("SetItems cursor = %d, want first selectable", l.Cursor)
	}
	l.Up()
	if l.Cursor != 1 {
		t.Fatalf("up from head skipped group, cursor = %d", l.Cursor)
	}
	l.Down()
	l.Down()
	l.Down()
	if l.Cursor != 3 {
		t.Fatalf("down end cursor = %d", l.Cursor)
	}
	l.Down()
	if l.Cursor != 3 {
		t.Fatalf("tail cursor = %d", l.Cursor)
	}
}

func TestListDescAndBadgeFit(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	l := &List{Width: 24}
	l.SetItems([]Item{{Title: "repo", Desc: "a very long path segment", Badge: "ok"}})
	rows := strings.Split(ansi.Strip(l.View()), "\n")
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if w := runeWidth(rows[0]); w != 24 {
		t.Fatalf("row width = %d, want 24: %q", w, rows[0])
	}
	if !strings.Contains(rows[0], "[ok]") {
		t.Fatalf("badge missing: %q", rows[0])
	}
	if !strings.Contains(rows[0], "…") {
		t.Fatalf("desc not ellipsis-clipped: %q", rows[0])
	}
}

func TestScrollerWindowAndCues(t *testing.T) {
	s := &Scroller{Total: 20, Height: 5}
	if start, end := s.Window(); start != 0 || end != 5 {
		t.Fatalf("head window = %d..%d", start, end)
	}
	if _, down := s.Cues(); down != true {
		t.Fatalf("head cues")
	}
	s.EnsureVisible(10)
	if start, _ := s.Window(); start != 6 {
		t.Fatalf("ensure window start = %d", start)
	}
	s.Bottom()
	if start, end := s.Window(); start != 15 || end != 20 {
		t.Fatalf("bottom window = %d..%d", start, end)
	}
	if ind := s.Indicator(); !strings.Contains(ind, "▲") {
		t.Fatalf("bottom indicator = %q", ind)
	}
}

func TestScrollerScrollbarStable(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	s := &Scroller{Total: 100, Height: 10}
	col := strings.Split(s.Scrollbar(10), "\n")
	if len(col) != 10 {
		t.Fatalf("scrollbar rows = %d", len(col))
	}
	for i, row := range col {
		if w := ansi.Width(ansi.Strip(row)); w != 1 {
			t.Fatalf("scrollbar row %d width = %d", i, w)
		}
	}
	s.SetOffset(50)
	col2 := strings.Split(s.Scrollbar(10), "\n")
	thumbAt := func(rows []string) int {
		for i, row := range rows {
			if ansi.Strip(row) == "█" {
				return i
			}
		}
		return -1
	}
	if thumbAt(col) == thumbAt(col2) {
		t.Fatalf("thumb did not move with offset")
	}
}

func TestModalScrollableBody(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := &Modal{Title: "long", Scrollable: true, Width: 30, Height: 8}
	for i := 0; i < 20; i++ {
		m.Lines = append(m.Lines, "line "+string(rune('a'+i)))
	}
	out := ansi.Strip(m.View())
	rows := strings.Split(out, "\n")
	if len(rows) != 8 {
		t.Fatalf("modal rows = %d, want 8", len(rows))
	}
	joined := strings.Join(rows, "\n")
	// Height 8 → 5 body rows: the head window shows lines a–e, not f.
	for _, want := range []string{"line a", "line b", "line c", "line d", "line e"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("head window missing %q", want)
		}
	}
	if strings.Contains(joined, "line f") {
		t.Fatalf("head window leaked rows: %q", joined)
	}
	m.Scroll(1)
	out = ansi.Strip(m.View())
	if strings.Contains(out, "line a") {
		t.Fatalf("scroll +1 kept line a")
	}
	m.Scroll(100)
	if got := ansi.Strip(m.View()); strings.Contains(got, "line t") == false {
		t.Fatalf("bottom window missing last line")
	}
}

func TestModalErrorRowPinned(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := &Modal{Title: "x", Error: "bad input", Scrollable: true, Width: 24, Height: 6}
	for i := 0; i < 10; i++ {
		m.Lines = append(m.Lines, "row")
	}
	m.Scroll(100)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "bad input") {
		t.Fatalf("pinned error scrolled away: %q", out)
	}
}

func TestPanelFooterSlot(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	p := NewPanel("t", false).SetContent("a", "b").SetFooter("▲ 1 of 2")
	p.Width, p.Height = 12, 6
	out := ansi.Strip(p.View())
	rows := strings.Split(out, "\n")
	if len(rows) != 6 {
		t.Fatalf("panel rows = %d", len(rows))
	}
	if !strings.Contains(rows[len(rows)-2], "▲ 1 of 2") {
		t.Fatalf("footer missing above bottom edge: %q", out)
	}
}

func TestRailGlyphAndScroll(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	r := &Rail{
		Rows: []RailRow{
			{Glyph: theme.GlyphBranch, Label: "one"},
			{Glyph: theme.GlyphBranch, Label: "two"},
			{Glyph: theme.GlyphBranch, Label: "three"},
			{Label: "four"},
			{Label: "five"},
		},
		Active: 4,
		Width:  12,
		Height: 5,
		Foot:   "[ ] sections",
	}
	// ensureActive keeps the ACTIVE row (five) visible; "one" scrolls
	// out and the cue rides the foot row.
	out := ansi.Strip(r.View())
	rows := strings.Split(out, "\n")
	if len(rows) != 5 {
		t.Fatalf("rail rows = %d, want 5 (foot own row)", len(rows))
	}
	if !strings.Contains(rows[len(rows)-1], "[ ] sections") {
		t.Fatalf("foot missing on last row")
	}
	if strings.Contains(out, "one") {
		t.Fatalf("overflow row not scrolled: %q", out)
	}
	if !strings.Contains(out, "five") {
		t.Fatalf("active row not kept visible: %q", out)
	}
	if !strings.Contains(out, "▲") {
		t.Fatalf("scroll cue missing on foot: %q", out)
	}
	if !strings.Contains(out, "⎇") {
		t.Fatalf("glyph slot missing: %q", out)
	}
}

func TestEllipsisClipMarker(t *testing.T) {
	if got := ClipEllipsis("abcdef", 4); got != "abc…" {
		t.Fatalf("clip = %q", got)
	}
	if got := ClipEllipsis("ab", 4); got != "ab" {
		t.Fatalf("short clip = %q", got)
	}
	if got := ClipEllipsis("abcdef", 1); got != "" {
		t.Fatalf("tiny clip = %q", got)
	}
}

func TestPanelRightEdgeScrollbar(t *testing.T) {
	body := make([]string, 20)
	for i := range body {
		body[i] = "row"
	}
	p := NewPanel("t", true).SetContent(body...)
	p.Width, p.Height = 20, 7 // body budget = 5
	p.SetScroll(Scroller{Total: 20, Height: 5, offset: 0})
	v := ansi.Strip(p.View())
	lines := strings.Split(v, "\n")
	// Body rows are lines[1..5]; count thumb cells on the right edge.
	thumbs, track := 0, 0
	for _, ln := range lines[1:6] {
		switch string([]rune(ln)[len([]rune(ln))-1]) {
		case "█":
			thumbs++
		case "│":
			track++
		}
	}
	if thumbs != 1 {
		t.Fatalf("thumbs = %d, want 1 (offset 0, exact window)\n%s", thumbs, v)
	}
	if track != 4 {
		t.Fatalf("track = %d, want 4\n%s", track, v)
	}
	// A scroller that fits leaves the edge plain (no thumb).
	fit := NewPanel("t", true).SetContent("a", "b")
	fit.Width, fit.Height = 12, 6
	if strings.Contains(ansi.Strip(fit.SetScroll(Scroller{Total: 2, Height: 4}).View()), "█") {
		t.Fatal("fitted scroller must not paint a thumb")
	}
}
