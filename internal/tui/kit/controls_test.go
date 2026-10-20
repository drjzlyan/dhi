package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestInputEditsAndKeepsCursor(t *testing.T) {
	in := NewInput("ad", "")
	for _, k := range []string{"left", "x", "end", "a", "home", "backspace", "delete"} {
		in.HandleKey(k)
	}
	if in.Value != "xda" || in.Cursor() != 0 {
		t.Fatalf("value %q cursor %d", in.Value, in.Cursor())
	}
	if in.HandleKey("enter") || in.HandleKey("tab") {
		t.Fatal("enter/tab must be left to the caller")
	}
	if in.HandleKey("ctrl+j") {
		t.Fatal("single-line input must not take ctrl+j")
	}
	in.HandleKey("paste:a\nb")
	if strings.Contains(in.Value, "\n") {
		t.Fatalf("single-line paste kept a newline: %q", in.Value)
	}
}

func TestMultilineInputGrowsARowPerLine(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	in := NewInput("one", "")
	in.Multiline, in.Focused = true, true
	in.HandleKey("ctrl+j")
	in.HandleKey("t")
	rows := in.View(20)
	if len(rows) != 2 {
		t.Fatalf("rows = %q", rows)
	}
	for _, r := range rows {
		if ansi.Width(r) != 20 {
			t.Fatalf("row width %d, want 20: %q", ansi.Width(r), ansi.Strip(r))
		}
	}
	if !strings.Contains(ansi.Strip(rows[1]), "t"+theme.GlyphCursor) {
		t.Fatalf("cursor not on the second line: %q", ansi.Strip(rows[1]))
	}
}

func TestInputScrollsToKeepCursorVisible(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	in := NewInput(strings.Repeat("abcdefghij", 5), "")
	in.Focused = true
	row := ansi.Strip(in.View(16)[0])
	if !strings.HasSuffix(strings.TrimRight(row, " "), theme.GlyphCursor) || !strings.Contains(row, "…") {
		t.Fatalf("long value did not scroll to the cursor: %q", row)
	}
}

func TestChecklistCounts(t *testing.T) {
	c := Checklist{Items: []ChecklistItem{{State: ItemDone}, {State: ItemSkipped}, {}}}
	if c.Done() != 1 || c.Settled() != 2 {
		t.Fatalf("done %d settled %d", c.Done(), c.Settled())
	}
}

func TestCardButtonSpansAreCardRelative(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	rows, spans := Card{Title: "Git identity", Body: "Commits carry this name.", Buttons: []Button{{Key: "⏎", Label: "Use it"}}}.View(40)
	if len(spans) != 1 {
		t.Fatalf("spans %+v", spans)
	}
	s := spans[0]
	got := string([]rune(ansi.Strip(rows[s.Line]))[s.From:s.To])
	if got != " ⏎ Use it " {
		t.Fatalf("span covers %q", got)
	}
	for _, r := range rows {
		if ansi.Width(r) != 40 {
			t.Fatalf("card row %d cells, want 40: %q", ansi.Width(r), ansi.Strip(r))
		}
	}
}

func TestGoldenComponents(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	var out []string
	bl, _ := Buttons([]Button{{Key: "⏎", Label: "Continue", Focused: true}, {Key: "e", Label: "Edit", Variant: theme.ButtonSecondary},
		{Key: "s", Label: "Skip", Variant: theme.ButtonGhost}, {Key: "i", Label: "Install", Disabled: true}}, 60)
	out = append(out, bl...)
	name := NewInput("Ada Lovelace", "")
	name.Focused = true
	out = append(out, name.View(30)...)
	email := Input{Placeholder: "you@example.com", Help: "Used on every commit you make here."}
	out = append(out, email.View(30)...)
	out = append(out, Checkbox("Co-author trailer", true, false)+"  "+Radio("squad", true, true)+"  "+Toggle("free", true))
	out = append(out, Badge(theme.BadgeOK, "installed")+" "+Badge(theme.BadgeError, "missing")+" "+Badge(theme.BadgeBusy, "working"))
	card, _ := Card{Title: "Coding CLI", Body: "claude 2.1 found.", Badge: "ready", BadgeKind: theme.BadgeOK, Selected: true}.View(40)
	out = append(out, card...)
	out = append(out, Checklist{Focus: true, Cursor: 1, Items: []ChecklistItem{
		{Title: "Git identity", Why: "sign your commits", State: ItemDone},
		{Title: "Create a workspace", Why: "group your repos"},
		{Title: "Integrations", Why: "connect GitHub", State: ItemSkipped},
	}}.View(60)...)
	golden.Snapshot(t, "components", strings.Join(out, "\n"))
}

func TestMultilineInputSoftWraps(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	in := NewInput(strings.Repeat("x", 25), "")
	in.Multiline, in.Focused = true, true
	rows := in.View(13) // 10 cells of text per row
	if len(rows) != 3 {
		t.Fatalf("25 runes at 10 per row = 3 rows, got %d: %q", len(rows), rows)
	}
	if !strings.Contains(ansi.Strip(rows[2]), "xxxxx"+theme.GlyphCursor) {
		t.Fatalf("cursor must end the last row: %q", ansi.Strip(rows[2]))
	}
	full := NewInput(strings.Repeat("y", 10), "")
	full.Multiline, full.Focused = true, true
	if r := full.View(13); len(r) != 2 || !strings.Contains(ansi.Strip(r[1]), theme.GlyphCursor) {
		t.Fatalf("cursor after a full row gets its own row: %q", r)
	}
}
