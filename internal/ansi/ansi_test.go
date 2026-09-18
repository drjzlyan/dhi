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
