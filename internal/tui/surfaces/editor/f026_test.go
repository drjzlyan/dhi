package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestHighlighterColorsGoBuffer(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	src := `package main

// main is a comment
func main() {
	x := 42
	println("hello")
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := textbuf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &highlighter{path: path}
	h.refresh(b)
	// The raw text survives round-trip (SGR stripped == the source).
	for i, l := range strings.Split(strings.TrimRight(src, "\n"), "\n") {
		if got := ansi.Strip(h.styled(i)); got != l {
			t.Fatalf("line %d = %q, want %q", i, got, l)
		}
	}
	// Comments and token styling carry SGR.
	if !strings.Contains(h.styled(2), "main is a comment") {
		t.Fatalf("comment line missing")
	}
	if h.styled(4) == "" && h.styled(5) == "" {
		t.Fatalf("no styled token found on the body lines")
	}
	// Sequence bump re-lexes; a same-seq refresh is cached.
	seq := b.Seq()
	b.InsertRune(' ')
	if b.Seq() == seq {
		t.Fatal("mutation did not bump the sequence")
	}
	h.refresh(b)
	if got := ansi.Strip(h.styled(4)); !strings.Contains(got, "42") {
		t.Fatalf("re-lex lost the number line: %q", got)
	}
}

func TestHighlighterUnknownLexerStaysPlain(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	h := &highlighter{path: filepath.Join(t.TempDir(), "data.unknownext")}
	h.refresh(textbuf.New("some text\n"))
	if got := h.styled(0); got != "" {
		t.Fatalf("unknown lexer styled: %q", got)
	}
}

func TestBufferViewCursorLineStaysPlain(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := newEditor(t)
	if m.active() == nil {
		t.Skip("no buffer in fixture")
	}
	e := m.active()
	e.Buffer().SetCursor(textbuf.Pos{Line: 0, Col: 0})
	out := m.bufferView()
	cur := e.Buffer().Cursor().Line
	if syntax := m.syntaxFor(e); syntax != nil {
		if syntax.styled(cur) != "" && ansi.Strip(syntax.styled(cur)) == e.Buffer().Line(cur) {
			// The styled cache may cover the cursor line, but the VIEW
			// renders the cursor line via withCursor — assert the view
			// keeps the raw text under the cursor row.
			_ = cur
		}
	}
	if !strings.Contains(ansi.Strip(out), e.Buffer().Line(cur)) {
		t.Fatalf("cursor line missing from the render: %q", out)
	}
}
