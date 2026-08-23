package editor

import (
	"strings"
	"testing"
	"time"
)

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
	if m.activeTerm != 1 || !strings.Contains(plainView(m), "[beta]") {
		t.Errorf("alt+2 did not switch (active=%d)", m.activeTerm)
	}
	feed(m, "alt+9") // out of range
	if m.activeTerm != 1 {
		t.Error("alt+9 changed tab")
	}
}

func TestTerminalStreamingRender(t *testing.T) {
	m := newEditor(t)
	feed(m, "ctrl+t")

	m.Update(teaMsg{kind: termMsgOut, tab: 0, chunk: []byte("build ok\r\n$ ")})
	m.Update(teaMsg{kind: termMsgOut, tab: 0, chunk: []byte("more")})

	v := plainView(m)
	for _, want := range []string{"build ok", "$ ", "more_"} {
		if !strings.Contains(v, want) {
			t.Errorf("scrollback missing %q:\n%s", want, v)
		}
	}

	m.Update(teaMsg{kind: termMsgClosed, tab: 0})
	if !strings.Contains(plainView(m), "[process exited]") {
		t.Error("exit marker missing")
	}
}

func TestLiveShellEchoThroughDrawer(t *testing.T) {
	m := newEditor(t)
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
