package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// TestRawBytesNormalize decodes the bytes real terminals send (F-101)
// with the same decoder Bubble Tea uses, then normalizes them the way the
// shell does, so a binding is tested against what a Mac actually emits.
func TestRawBytesNormalize(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		takingText bool
		want       string
	}{
		{"Terminal.app Option+1 without Meta", "¡", false, "alt+1"},
		{"Option+1 while typing stays text", "¡", true, "¡"},
		{"Option+1 with Meta", "\x1b1", false, "alt+1"},
		{"Terminal.app Option+left with Meta", "\x1bb", true, "alt+left"},
		{"Terminal.app Option+right with Meta", "\x1bf", true, "alt+right"},
		{"iTerm2/xterm alt+left", "\x1b[1;3D", true, "alt+left"},
		{"xterm ctrl+right", "\x1b[1;5C", true, "ctrl+right"},
		{"kitty CSI-u alt+1", "\x1b[49;3u", false, "alt+1"},
		{"kitty CSI-u ctrl+k", "\x1b[107;5u", false, "ctrl+k"},
		{"ctrl+/ as ctrl+_", "\x1f", false, "ctrl+_"},
		{"Option+backspace with Meta", "\x1b\x7f", true, "alt+backspace"},
		{"fn+left (home)", "\x1b[H", true, "home"},
	}
	for _, c := range cases {
		var d uv.EventDecoder
		n, ev := d.Decode([]byte(c.raw))
		if n != len(c.raw) {
			t.Errorf("%s: decoded %d of %d bytes", c.name, n, len(c.raw))
			continue
		}
		kp, ok := ev.(uv.KeyPressEvent)
		if !ok {
			t.Errorf("%s: %T, want a key press", c.name, ev)
			continue
		}
		got, _ := kit.NormalizeKey(keyString(tea.KeyPressMsg(kp)), c.takingText, false)
		if got != c.want {
			t.Errorf("%s: %q → %q, want %q", c.name, c.raw, got, c.want)
		}
	}
}
