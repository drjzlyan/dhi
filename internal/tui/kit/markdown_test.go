package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestLooksLikeMarkdown(t *testing.T) {
	for s, want := range map[string]bool{
		"plain words, nothing else":       false,
		"it costs 3 * 4 = 12":             false,
		"run `go test` first":             true,
		"```go\nx := 1\n```":              true,
		"- one\n- two":                    true,
		"1. first\n2. second":             true,
		"## Plan":                         true,
		"> quoted":                        true,
		"see [docs](https://x.dev)":       true,
		"this is **important**":           true,
		"| a | b |\n|---|---|\n| 1 | 2 |": true,
	} {
		if got := LooksLikeMarkdown(s); got != want {
			t.Errorf("LooksLikeMarkdown(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	out := RenderMarkdown("Here is the fix:\n\n```go\nfunc add(a, b int) int {\n\treturn a + b\n}\n```", 40)
	plain := ansi.Strip(out)
	if strings.Contains(plain, "```") {
		t.Fatalf("fence markers leaked:\n%s", plain)
	}
	for _, want := range []string{"Here is the fix:", "go", "func add(a, b int) int {", "    return a + b"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q:\n%s", want, plain)
		}
	}
	// Code rows paint the full width on the code shade.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(l), "return a + b") && ansi.Width(l) != 40 {
			t.Fatalf("code row width = %d, want 40: %q", ansi.Width(l), l)
		}
	}
}

func TestRenderMarkdownInlineAndLists(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	plain := ansi.Strip(RenderMarkdown("Run `make verify` then:\n\n- **fix** the test\n- push\n\n1. one\n2. two", 40))
	for _, want := range []string{"Run make verify then:", "• fix the test", "• push", "1. one", "2. two"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q:\n%s", want, plain)
		}
	}
	if strings.ContainsAny(plain, "`*") {
		t.Fatalf("markup leaked:\n%s", plain)
	}
}

func TestRenderMarkdownStaysInWidth(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	src := "A very long paragraph " + strings.Repeat("word ", 40) + "\n\n```\n" +
		strings.Repeat("x", 90) + "\n```\n\n| name | value |\n|---|---|\n| " + strings.Repeat("k", 50) + " | v |"
	for _, w := range []int{20, 33, 60} {
		for _, l := range strings.Split(RenderMarkdown(src, w), "\n") {
			if got := ansi.Width(l); got > w {
				t.Fatalf("width %d: line is %d cells: %q", w, got, ansi.Strip(l))
			}
		}
	}
}

func TestRenderMarkdownLinkAndQuote(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	plain := ansi.Strip(RenderMarkdown("> careful here\n\nSee [the ADR](docs/adr/0035.md).", 60))
	if !strings.Contains(plain, "▌ careful here") {
		t.Fatalf("quote bar missing:\n%s", plain)
	}
	if !strings.Contains(plain, "the ADR (docs/adr/0035.md)") {
		t.Fatalf("link target missing:\n%s", plain)
	}
}

func TestChatMarkdownSkipsProse(t *testing.T) {
	if got := ChatMarkdown("just a sentence", 40); got != "" {
		t.Fatalf("prose went through the renderer: %q", got)
	}
}

func TestTranscriptSuffixPlacement(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	tr := &Transcript{Width: 60, Markdown: ChatMarkdown, Rows: []TrnRow{
		{Author: "you", Kind: TrnHuman, Text: "short note", Suffix: "(edited)", Markdown: true},
		{Author: "scout", Kind: TrnAgent, Text: "```go\nx := 1\n```", Suffix: "[+1]", Markdown: true},
	}}
	lines := tr.View()
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "short note (edited)") {
		t.Fatalf("prose suffix should share the line:\n%s", plain)
	}
	for _, l := range lines {
		if s := ansi.Strip(l); strings.Contains(s, "x := 1") && strings.Contains(s, "[+1]") {
			t.Fatalf("suffix painted into the code block:\n%s", plain)
		}
	}
	if !strings.Contains(plain, "[+1]") {
		t.Fatalf("suffix missing:\n%s", plain)
	}
}
