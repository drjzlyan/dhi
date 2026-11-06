package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/textbuf"
)

// The surface's z, g, K, ] and [ chords must not take a key the buffer is
// waiting for as data (an f/t target, a text-object character, a block r
// character), and visual g-motions reach the buffer.
func TestChordKeysAreDataWhenBufferWaits(t *testing.T) {
	cases := []struct {
		name, text string
		keys       []string
		want       string // buffer text after
		col        int    // cursor column on line 0, -1 = unchecked
		mode       textbuf.Mode
	}{
		{"fg", "abgcd", []string{"f", "g"}, "abgcd", 2, textbuf.ModeNormal},
		{"tg", "abgcd", []string{"t", "g"}, "abgcd", 1, textbuf.ModeNormal},
		{"fK", "abKcd", []string{"f", "K"}, "abKcd", 2, textbuf.ModeNormal},
		{"fz", "abzcd", []string{"f", "z"}, "abzcd", 2, textbuf.ModeNormal},
		{"f]", "ab]cd", []string{"f", "]"}, "ab]cd", 2, textbuf.ModeNormal},
		{"f[", "ab[cd", []string{"f", "["}, "ab[cd", 2, textbuf.ModeNormal},
		{"dfg", "abgcd", []string{"d", "f", "g"}, "cd", 0, textbuf.ModeNormal},
		{"vfg", "abgcd", []string{"v", "f", "g"}, "abgcd", 2, textbuf.ModeVisual},
		{"block rg", "ab\ncd", []string{"ctrl+v", "j", "r", "g"}, "gb\ngd", -1, textbuf.ModeNormal},
		{"block rz", "ab\ncd", []string{"ctrl+v", "j", "r", "z"}, "zb\nzd", -1, textbuf.ModeNormal},
		{"block r]", "ab\ncd", []string{"ctrl+v", "j", "r", "]"}, "]b\n]d", -1, textbuf.ModeNormal},
		{"block rK", "ab\ncd", []string{"ctrl+v", "j", "r", "K"}, "Kb\nKd", -1, textbuf.ModeNormal},
		{"di[", "x[ab]y", []string{"l", "l", "d", "i", "["}, "x[]y", -1, textbuf.ModeNormal},
		{"da]", "x[ab]y", []string{"l", "l", "d", "a", "]"}, "xy", -1, textbuf.ModeNormal},
		{"yi[", "x[ab]y", []string{"l", "l", "y", "i", "["}, "x[ab]y", -1, textbuf.ModeNormal},
		{"ci[", "x[ab]y", []string{"l", "l", "c", "i", "["}, "x[]y", -1, textbuf.ModeInsert},
		{"diz", "x[ab]y", []string{"l", "l", "d", "i", "z"}, "x[ab]y", -1, textbuf.ModeNormal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := openText(t, c.text, false)
			feed(m, c.keys...)
			e := m.active()
			if got := e.Buffer().Text(); got != c.want {
				t.Errorf("text = %q, want %q", got, c.want)
			}
			if c.col >= 0 && e.Buffer().Cursor().Col != c.col {
				t.Errorf("col = %d, want %d", e.Buffer().Cursor().Col, c.col)
			}
			if e.Mode() != c.mode {
				t.Errorf("mode = %v, want %v", e.Mode(), c.mode)
			}
			if m.pendingZ || m.lspPendingG || m.pendingBracket != "" {
				t.Errorf("a surface chord is armed: z=%v g=%v bracket=%q", m.pendingZ, m.lspPendingG, m.pendingBracket)
			}
		})
	}
}

func TestGcommentWithTextObject(t *testing.T) {
	m := openText(t, "x[ab]y\nz\n", false)
	feed(m, "l", "l", "g", "c", "i", "[")
	if m.lspPendingG || m.pendingBracket != "" {
		t.Fatal("a surface chord is armed")
	}
	if m.active().Mode() != textbuf.ModeNormal || !strings.Contains(m.active().Buffer().Text(), "//") {
		t.Errorf("gci[ did not comment: %q", m.active().Buffer().Text())
	}
}

func TestVisualGGReachesBuffer(t *testing.T) {
	m := openText(t, "a\nb\nc\n", false)
	feed(m, "G", "v", "g", "g")
	e := m.active()
	if e.Mode() != textbuf.ModeVisual || e.Buffer().Cursor().Line != 0 {
		t.Fatalf("mode %v line %d", e.Mode(), e.Buffer().Cursor().Line)
	}
	if m.lspPendingG {
		t.Error("g still armed")
	}
}
