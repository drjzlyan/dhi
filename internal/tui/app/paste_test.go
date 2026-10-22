package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// copySurface hands text to the shell's copy seam.
type copySurface struct {
	stubSurface
	copy string
}

func (s *copySurface) TakeCopy() (string, bool) {
	c := s.copy
	s.copy = ""
	return c, c != ""
}

// F-103: a bracketed paste reaches the focused thing as one "paste:"
// key, CRLF normalized, and never acts as a binding (a pasted "2" does
// not switch views, a pasted "?" does not open help).
func TestPasteGoesToTheFocusedSurfaceOnly(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	s := &stubSurface{id: "ws", title: "WS", consumeKeys: true}
	a := New("t", s, &stubSurface{id: "two", title: "Two"})
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.Update(tea.PasteMsg{Content: "2?\r\nok"})
	if a.active != 0 || a.showHelp {
		t.Fatal("a paste acted as a key binding")
	}
	if got := s.keys[len(s.keys)-1]; got != "paste:2?\nok" {
		t.Fatalf("surface got %q", got)
	}
	a.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	a.Update(tea.PasteMsg{Content: "theme"})
	if a.palette == nil || a.palette.Query() != "theme" {
		t.Fatal("with the palette open, paste fills its query")
	}
}

func TestPasteTooLargeIsRefused(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	s := &stubSurface{id: "ws", title: "WS", consumeKeys: true}
	a := New("t", s)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.Update(tea.PasteMsg{Content: strings.Repeat("x", maxPaste+1)})
	if len(s.keys) != 0 || a.toast == nil {
		t.Fatal("an oversized paste must be refused with a toast")
	}
}

func TestCopySeamEmitsClipboardAndToast(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	var osGot string
	kit.SetOSClipboard(func(s string) error { osGot = s; return nil })
	t.Cleanup(func() { kit.SetOSClipboard(nil) })
	s := &copySurface{stubSurface: stubSurface{id: "ws", title: "WS", consumeKeys: true}}
	a := New("t", s)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	s.copy = "hello"
	_, cmd := a.Update(keyPress("Y"))
	if cmd == nil {
		t.Fatal("no clipboard command")
	}
	for _, m := range drain(cmd) {
		a.Update(m)
	}
	if osGot != "hello" {
		t.Fatalf("OS clipboard got %q", osGot)
	}
	if a.toast == nil || a.toast.Title != "Copied" {
		t.Fatalf("toast = %+v", a.toast)
	}
}

// drain runs cmd (and nested batches) and returns the messages.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drain(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}
