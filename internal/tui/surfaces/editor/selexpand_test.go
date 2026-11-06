package editor

import (
	"strings"
	"testing"
)

func lspRange(l1, c1, l2, c2 int) map[string]any {
	return map[string]any{
		"start": map[string]any{"line": l1, "character": c1},
		"end":   map[string]any{"line": l2, "character": c2},
	}
}

func selText(m *Model) string {
	s, _, _, _ := m.active().Selection()
	return s
}

// The opened file is "package main\n"; the cursor starts at 0,0.

func TestExpandUsesServerSelectionRange(t *testing.T) {
	m, srv := openWithCfg(t, func(f *goplusFake) {
		f.selCap = true
		f.selChain = []map[string]any{lspRange(0, 0, 0, 7), lspRange(0, 0, 0, 12), lspRange(0, 0, 1, 0)}
	})
	feed(m, "alt+o")
	waitFor(t, m, func() bool { return selText(m) == "package" }, "first server range")
	if got := m.active().Message(); !strings.Contains(got, "language server") {
		t.Errorf("message = %q", got)
	}
	feed(m, "alt+o")
	waitFor(t, m, func() bool { return selText(m) == "package main" }, "second server range")
	feed(m, "alt+o") // the third ends at 1,0: a line break cannot be selected, so nothing is larger
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "nothing larger") }, "no larger range")
	feed(m, "alt+i")
	if got := selText(m); got != "package" {
		t.Errorf("shrink = %q", got)
	}
	feed(m, "alt+i")
	if m.active().Mode().String() != "NORMAL" {
		t.Errorf("full shrink should leave visual mode, got %v", m.active().Mode())
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.selAsked != 3 {
		t.Errorf("selectionRange requests = %d, want 3", srv.selAsked)
	}
}

func TestExpandFallsBackWhenServerLacksSelectionRange(t *testing.T) {
	// Capabilities reported (save options) but no selectionRangeProvider.
	m, srv := openWithCfg(t, func(f *goplusFake) { f.didSaveOpts = map[string]any{} })
	feed(m, "alt+o")
	if got := m.active().Message(); !strings.Contains(got, "structural") || !strings.Contains(got, "server has no selectionRange") {
		t.Errorf("message = %q", got)
	}
	if got := selText(m); got != "package" {
		t.Errorf("structural word = %q", got)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.selAsked != 0 {
		t.Error("asked a server that did not advertise selectionRange")
	}
}

func TestExpandFallsBackOnServerError(t *testing.T) {
	m, _ := openWithCfg(t, func(f *goplusFake) { f.selCap, f.selFail = true, true })
	feed(m, "alt+o")
	waitFor(t, m, func() bool { return selText(m) == "package" }, "structural fallback")
	if got := m.active().Message(); !strings.Contains(got, "selectionRange failed") {
		t.Errorf("message = %q", got)
	}
}

func TestExpandFallsBackWhenServerRangeNotLarger(t *testing.T) {
	m, _ := openWithCfg(t, func(f *goplusFake) {
		f.selCap = true
		f.selChain = []map[string]any{lspRange(0, 0, 0, 1)} // just the character already selected
	})
	feed(m, "alt+o")
	waitFor(t, m, func() bool { return selText(m) == "package" }, "structural fallback")
	if got := m.active().Message(); !strings.Contains(got, "server has no larger range") {
		t.Errorf("message = %q", got)
	}
}

func TestExpandWithoutLanguageServerNamesTheRung(t *testing.T) {
	m, _ := pairFile(t, "xy := f(\"a b\")\n")
	feed(m, "alt+o")
	if got := m.active().Message(); !strings.Contains(got, "no language server") || !strings.Contains(got, "word") {
		t.Errorf("message = %q", got)
	}
	feed(m, "alt+o")
	if !strings.Contains(m.active().Message(), "structural") {
		t.Errorf("message = %q", m.active().Message())
	}
	feed(m, "alt+i")
	feed(m, "alt+i")
	feed(m, "alt+i")
	if got := m.active().Message(); !strings.Contains(got, "nothing to shrink") {
		t.Errorf("message = %q", got)
	}
}

func TestTextObjectKeysThroughSurface(t *testing.T) {
	m, _ := pairFile(t, "call(a, b)\n\nz\n")
	feed(m, "f", ",", "d", "i", "(")
	if got := m.active().Buffer().Text(); got != "call()\n\nz\n" {
		t.Fatalf("di( = %q", got)
	}
	feed(m, "g", "c", "i", "p") // the g-chord is the surface's; the object follows it
	if got := m.active().Buffer().Text(); got != "// call()\n\nz\n" {
		t.Fatalf("gcip = %q", got)
	}
}
