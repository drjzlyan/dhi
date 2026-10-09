package editor

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// liveEditor builds an editor whose terminal sessions get an explicit
// environment (ADR-0011: tests opt in explicitly, like production
// passes the hermetic toolchain env).
func liveEditor(t *testing.T) *Model {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithTermEnv(os.Environ()))
	m.Resize(100, 30)
	return m
}

func TestDrawerToggleCycle(t *testing.T) {
	m := newEditor(t)

	feed(m, "ctrl+t")
	if !m.drawerOpen || !m.termFocus {
		t.Fatal("ctrl+t did not open+focus drawer")
	}
	if len(m.terms) != 2 { // one per member repo
		t.Fatalf("tabs = %d, want 2", len(m.terms))
	}
	if !strings.Contains(plainView(m), "terminal") {
		t.Error("drawer not rendered")
	}

	feed(m, "ctrl+t") // blur
	if !m.drawerOpen || m.termFocus {
		t.Fatal("second ctrl+t should blur")
	}
	feed(m, "ctrl+t") // close
	if m.drawerOpen {
		t.Fatal("third ctrl+t should close")
	}
}

func TestDrawerFocusSwallowsKeys(t *testing.T) {
	m := newEditor(t)
	cursorBefore := m.list.Cursor
	feed(m, "ctrl+t")

	for _, k := range []string{"j", "k", "/", "s", "i", "enter"} {
		if !m.HandleKey(k) {
			t.Errorf("key %q not consumed by focused terminal", k)
		}
	}
	if m.list.Cursor != cursorBefore && m.mode != modeNav {
		t.Log("tree state changed while terminal focused")
	}
}

func TestAltDigitSwitchesTabs(t *testing.T) {
	m := newEditor(t)
	feed(m, "ctrl+t")
	if m.activeTerm != 0 {
		t.Fatalf("initial active = %d", m.activeTerm)
	}
	feed(m, "alt+2")
	if m.activeTerm != 1 || !strings.Contains(plainView(m), " beta ") {
		t.Errorf("alt+2 did not switch (active=%d)", m.activeTerm)
	}
	feed(m, "alt+9") // out of range
	if m.activeTerm != 1 {
		t.Error("alt+9 changed tab")
	}
}

func TestTerminalStreamingRender(t *testing.T) {
	m := liveEditor(t)
	feed(m, "ctrl+t")

	m.Update(teaMsg{kind: termMsgOut, tab: 0, chunk: []byte("build ok\r\n$ ")})
	m.Update(teaMsg{kind: termMsgOut, tab: 0, chunk: []byte("more")})

	v := plainView(m)
	for _, want := range []string{"build ok", "$ more"} {
		if !strings.Contains(v, want) {
			t.Errorf("screen missing %q:\n%s", want, v)
		}
	}
	// The real cursor cell (F-061) sits right after the typed text.
	if !regexp.MustCompile(`more(\x1b\[[0-9;]*m)*` + regexp.QuoteMeta(cursorStyle.Render(" "))).MatchString(m.View()) {
		t.Error("cursor cell not drawn after the prompt text")
	}

	m.Update(teaMsg{kind: termMsgClosed, tab: 0})
	if !strings.Contains(plainView(m), "[process exited]") {
		t.Error("exit marker missing")
	}
}

func TestLiveShellEchoThroughDrawer(t *testing.T) {
	m := liveEditor(t)
	feed(m, "ctrl+t")
	if len(m.terms) == 0 || m.terms[0].sess == nil {
		t.Skip("no session started")
	}

	typeKeys(m, "echo dhi-live-$((40+2))")
	feed(m, "enter")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.drainTerm()
		if strings.Contains(plainView(m), "dhi-live-42") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("live echo not observed:\n%s", plainView(m))
}

// TestDrawerRefusesWithoutHermeticEnv pins the visible refusal
// (ADR-0011): no host-env fallback session, the drawer names the fix.
func TestDrawerRefusesWithoutHermeticEnv(t *testing.T) {
	m := newEditor(t) // no WithTermEnv → hermetic env unavailable
	feed(m, "ctrl+t")
	v := plainView(m)
	if !strings.Contains(v, "hermetic environment unavailable") {
		t.Fatalf("refusal not visible:\n%s", v)
	}
	if len(m.terms) == 0 || m.terms[0].sess != nil {
		t.Fatal("no session may start without an explicit env")
	}
}

// TestFullScreenProgramTakesTheBody pins F-061: a program on the
// alternate screen gets the whole body, every row fits, and leaving it
// returns to the drawer.
func TestFullScreenProgramTakesTheBody(t *testing.T) {
	m := liveEditor(t)
	m.Resize(100, 30)
	feed(m, "ctrl+t")
	m.Update(teaMsg{kind: termMsgOut, tab: 0, chunk: []byte("\x1b[?1049h\x1b[2J\x1b[H~ vim buffer\x1b[30;1H-- INSERT --")})
	if !m.fullScreenTerm() {
		t.Fatal("alt screen did not go full screen")
	}
	if cols, rows := m.terms[0].screen.Size(); cols != 96 || rows != 27 {
		t.Fatalf("emulator not resized to the body: %dx%d", cols, rows)
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Fatalf("rows = %d, want 30", len(lines))
	}
	for i, l := range lines {
		if w := ansi.Width(l); w > 100 {
			t.Fatalf("row %d is %d cells", i, w)
		}
	}
	if v := plainView(m); !strings.Contains(v, "~ vim buffer") || !strings.Contains(v, "full screen") {
		t.Fatalf("full-screen view:\n%s", v)
	}
	m.Update(teaMsg{kind: termMsgOut, tab: 0, chunk: []byte("\x1b[?1049l")})
	if m.fullScreenTerm() || !strings.Contains(plainView(m), "terminal") {
		t.Fatal("leaving the alt screen must return to the drawer")
	}
}

func TestTermKeyEncoding(t *testing.T) {
	cases := map[string]string{
		"ctrl+a": "\x01", "ctrl+z": "\x1a", "ctrl+[": "\x1b", "alt+b": "\x1bb",
		"pgdown": "\x1b[6~", "delete": "\x1b[3~", "f5": "\x1b[15~", "home": "\x1b[H",
		"shift+tab": "\x1b[Z", "x": "x", "alt+left": "\x1b\x1b[D",
	}
	for k, want := range cases {
		if got, ok := termKeyBytes(k); !ok || string(got) != want {
			t.Errorf("%s → %q, want %q", k, got, want)
		}
	}
}
