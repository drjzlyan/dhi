package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

// F-159: ctrl+v reaches the buffer and draws the rectangle.
func TestBlockVisualGolden(t *testing.T) {
	m := newEditor(t)
	m.HandleKey("enter")
	m.HandleKey("down")
	m.HandleKey("enter")
	m.HandleKey("down")
	m.HandleKey("down")
	m.HandleKey("enter") // main.go
	feed(m, "i")
	typeKeys(m, "alpha beta gamma")
	feed(m, "enter")
	typeKeys(m, "one two")
	feed(m, "enter")
	typeKeys(m, "x")
	feed(m, "esc", "ctrl+home", "l", "ctrl+v", "j", "j", "l", "l")
	if !m.active().VisualBlock() {
		t.Fatal("ctrl+v did not start block visual")
	}
	top, bot, left, right, _ := m.active().BlockRange()
	if bot-top != 2 || right-left != 3 {
		t.Errorf("block %d..%d cells %d..%d", top, bot, left, right)
	}
	golden.Snapshot(t, "editor_block_visual_100x30", m.View())
	feed(m, "esc")
	if m.active().VisualBlock() {
		t.Error("esc leaves block visual")
	}
}

func TestMarkCells(t *testing.T) {
	if got := markCells("abcdef", 2, 4); !strings.Contains(got, "\x1b[7mcd") {
		t.Errorf("block cells: %q", got)
	}
	if got := markCells("ab", 5, 8); got != "ab" {
		t.Errorf("a short line is unmarked: %q", got)
	}
	if got := markCells("a世b", 1, 3); !strings.Contains(got, "\x1b[7m世") {
		t.Errorf("wide rune is one block: %q", got)
	}
}

// A block selection never lets a suggestion or an agent edit replace the
// text around the block (F-159).
func TestBlockSelectionGuardsReplacingFeatures(t *testing.T) {
	m := openText(t, "abcd\nefgh\nijkl\n", false)
	e := m.active()
	for _, k := range []string{"l", "ctrl+v", "j", "l"} {
		e.Key(k)
	}
	if !e.BlockSelection() {
		t.Fatal("not in block mode")
	}
	r, ok := m.currentRegion(0)
	if !ok || r.text != "abcd\nefgh" || r.from != 0 || r.to != 1 {
		t.Fatalf("region = %+v %v, want the block's whole lines", r, ok)
	}
	m.applySuggestion("XX")
	if got := e.Buffer().Text(); got != "abcd\nefgh\nijkl\n" {
		t.Fatalf("suggestion changed a block selection: %q", got)
	}
}
