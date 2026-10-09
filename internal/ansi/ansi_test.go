package ansi

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestStrip(t *testing.T) {
	if got := Strip("\x1b[31mred\x1b[0m"); got != "red" {
		t.Fatalf("Strip = %q", got)
	}
}

func TestClipPreservesStyles(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("abcdef")
	got := Clip(styled, 3)
	if vis := Strip(got); vis != "abc" {
		t.Fatalf("Clip visible = %q, want abc", vis)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Fatal("Clip dropped the style sequence")
	}
}

func TestClipExactAndEmpty(t *testing.T) {
	if got := Clip("abc", 10); got != "abc" {
		t.Fatalf("Clip short = %q", got)
	}
	if got := Clip("abc", 0); got != "" {
		t.Fatalf("Clip zero = %q", got)
	}
	// Escape sequences never count toward width.
	if got := Strip(Clip("\x1b[1mab\x1b[0mcd", 3)); got != "abc" {
		t.Fatalf("Clip with leading style = %q", got)
	}
}

func TestWidthCountsDisplayCells(t *testing.T) {
	// ASCII: display width == rune count.
	if got := Width("hello"); got != 5 {
		t.Fatalf("Width ascii = %d", got)
	}
	// Wide glyphs count as two cells.
	if got := Width("日本"); got != 4 {
		t.Fatalf("Width cjk = %d", got)
	}
	// Escape sequences are skipped in either input shape.
	if got := Width("\x1b[31m日本\x1b[0mx"); got != 5 {
		t.Fatalf("Width styled = %d", got)
	}
	if got := Width(Strip("\x1b[31m日本\x1b[0mx")); got != 5 {
		t.Fatalf("Width stripped = %d", got)
	}
}

func TestClipNeverOverflowsWideRunes(t *testing.T) {
	if got := Strip(Clip("日本", 3)); got != "日" {
		t.Fatalf("Clip wide = %q, want the single 2-cell rune", got)
	}
	if got := Strip(Clip("日本", 4)); got != "日本" {
		t.Fatalf("Clip wide exact = %q", got)
	}
}

func bgOf(cells []Cell) string {
	var b strings.Builder
	for _, c := range cells {
		if c.Bg {
			b.WriteByte('#')
		} else {
			b.WriteByte('.')
		}
	}
	return b.String()
}

func TestFillReappliesBackgroundAfterResets(t *testing.T) {
	bg := "\x1b[48;2;1;2;3m"
	in := "a\x1b[31mb\x1b[0mc\x1b[mD\x1b[49mE"
	got := Fill(in, bg)
	if g := bgOf(Cells(got)); g != "#####" {
		t.Fatalf("Fill cells = %s, want #####\n%q", g, got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatal("Fill must end with a reset")
	}
	if Strip(got) != "abcDE" {
		t.Fatalf("Fill changed text: %q", Strip(got))
	}
}

func TestFillKeepsInnerBackgroundAndExtendedFg(t *testing.T) {
	bg := "\x1b[48;2;1;2;3m"
	// an fg of black (38;2;0;0;0) must not read as a reset
	in := "\x1b[38;2;0;0;0mx\x1b[48;2;9;9;9my"
	got := Fill(in, bg)
	if strings.Count(got, bg) != 1 {
		t.Fatalf("Fill re-emitted bg needlessly: %q", got)
	}
}

func TestSlice(t *testing.T) {
	red := "\x1b[31m"
	in := red + "abcdef\x1b[0m"
	got := Slice(in, 2, 4)
	if Strip(got) != "cd" || !strings.HasPrefix(got, red) {
		t.Fatalf("Slice = %q", got)
	}
	if Slice("ab", 5, 9) != "" {
		t.Fatal("Slice past the end must be empty")
	}
	// wide rune straddling the start becomes a space
	if s := Strip(Slice("a漢b", 2, 4)); s != " b" {
		t.Fatalf("Slice wide = %q", s)
	}
}

func TestCellsTracksBackgroundAndReverse(t *testing.T) {
	in := "a\x1b[44mb\x1b[0mc\x1b[7md\x1b[27me漢"
	if g := bgOf(Cells(in)); g != ".#.#..." {
		t.Fatalf("Cells = %s", g)
	}
}
