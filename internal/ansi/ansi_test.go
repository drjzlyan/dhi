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
