package editor

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/tui/surfaces"
)

var _ surfaces.InputCapturer = (*Model)(nil)

func TestEditorCapturesInputWhereTextIsTyped(t *testing.T) {
	m := newEditor(t)
	if m.CapturesInput() {
		t.Fatal("the file tree is navigation: digits must switch views")
	}
	feed(m, "enter", "down", "down", "enter") // open a buffer, focus it
	if !m.CapturesInput() {
		t.Fatal("a focused buffer owns plain keys (insert text, vim counts, tab)")
	}
	feed(m, "esc") // back to the tree
	if m.CapturesInput() {
		t.Fatal("leaving the buffer must hand digits back to the shell")
	}
	feed(m, "/")
	if !m.CapturesInput() {
		t.Fatal("the file finder is a text prompt")
	}
}

func TestEditorChatAndTerminalCaptureInput(t *testing.T) {
	h := newChatEditor(t, "ok")
	h.openFocused()
	if !h.m.CapturesInput() {
		t.Fatal("a focused chat composer owns plain keys")
	}
	h.m.HandleKey("esc")
	if h.m.CapturesInput() {
		t.Fatal("an unfocused chat sidebar must not capture")
	}
}
