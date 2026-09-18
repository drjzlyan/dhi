package kit

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestTranscriptLayout(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	day1 := time.Date(2026, 9, 18, 9, 30, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 19, 14, 5, 0, 0, time.UTC)
	tr := &Transcript{Width: 60, Rows: []TrnRow{
		{Author: "you", Kind: TrnHuman, Text: "hello there", At: day1},
		{Author: "scout", Kind: TrnAgent, Text: strings.Repeat("word ", 20), Thread: true, At: day1.Add(5 * time.Minute)},
		{Author: "bus", Kind: TrnSystem, Text: "scout joined", At: day2, Sep: "new messages"},
	}}
	rows := strings.Split(ansi.Strip(strings.Join(tr.View(), "\n")), "\n")
	joined := strings.Join(rows, "\n")
	// Day dividers: one for each day, the first row's divider included.
	if strings.Count(joined, "── Fri Sep 18 ──") != 1 {
		t.Fatalf("day1 divider missing: %q", joined)
	}
	if strings.Count(joined, "── Sat Sep 19 ──") != 1 {
		t.Fatalf("day2 divider missing")
	}
	// Separator banner renders above its row.
	if !strings.Contains(joined, "── new messages ──") {
		t.Fatalf("sep banner missing")
	}
	// Thread tag, stamps, authors all present.
	for _, want := range []string{"↳ scout", "09:30", "09:35", "14:05", "you", "scout joined"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("transcript missing %q:\n%s", want, joined)
		}
	}
	// Continuation lines align at the text column.
	for _, r := range rows {
		if w := ansi.Width(r); w > 60 {
			t.Fatalf("row overflows width: %q", r)
		}
	}
}

func TestTranscriptEmpty(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	if got := (&Transcript{Width: 40}).View(); got != nil {
		t.Fatalf("empty transcript rows = %v", got)
	}
}

func TestTranscriptMarkdownSeam(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	tr := &Transcript{Width: 40, Markdown: func(md string, w int) string {
		return "RENDERED(" + md + ")"
	}, Rows: []TrnRow{
		{Author: "scout", Kind: TrnAgent, Text: "```go\nx := 1\n```", Markdown: true},
	}}
	joined := strings.Join(tr.View(), "\n")
	if !strings.Contains(ansi.Strip(joined), "RENDERED") {
		t.Fatalf("markdown seam not used: %q", joined)
	}
	// Nil seam degrades to raw text, never silent.
	tr2 := &Transcript{Width: 40, Rows: []TrnRow{
		{Author: "scout", Kind: TrnAgent, Text: "```go\nx := 1\n```", Markdown: true},
	}}
	if !strings.Contains(ansi.Strip(strings.Join(tr2.View(), "\n")), "```go") {
		t.Fatalf("nil seam dropped the text")
	}
}

func TestWrapWordsHardBreaks(t *testing.T) {
	got := WrapWords(strings.Repeat("x", 25), 10)
	for _, l := range got {
		if runeWidth(l) > 10 {
			t.Fatalf("line overflows: %q", l)
		}
	}
	if len(got) != 3 {
		t.Fatalf("hard break rows = %d, want 3", len(got))
	}
	joined := strings.Join(got, "")
	if joined != strings.Repeat("x", 25) {
		t.Fatalf("hard break lost runes: %q", joined)
	}
	// Word wrap at spaces.
	got = WrapWords("alpha beta gamma", 10)
	if strings.Join(got, "|") != "alpha beta|gamma" {
		t.Fatalf("wrap = %q", got)
	}
	// Empty paragraphs keep blank rows.
	got = WrapWords("a\n\nb", 10)
	if len(got) != 3 || got[1] != "" {
		t.Fatalf("blank rows = %q", got)
	}
}
