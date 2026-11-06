package editor

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// markedSession fills the first drawer tab with n marked prompts; the
// prompts whose number is in failed exit with status 1. The tab has no
// live shell (no environment given), so only the screen is under test.
func markedSession(t *testing.T, n int, failed ...int) (*Model, *termTab) {
	t.Helper()
	m := newEditor(t)
	feed(m, "ctrl+t")
	tab := m.activeTermTab()
	cols, rows := m.termGeom(false)
	tab.screen.Resize(cols, rows) // an unstarted shell never syncs the size
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "\x1b]133;A\x07prompt%02d\r\n", i)
		code := 0
		for _, f := range failed {
			if f == i {
				code = 1
			}
		}
		fmt.Fprintf(&b, "\x1b]133;D;%d\x07", code)
	}
	tab.screen.Feed([]byte(b.String()))
	return m, tab
}

func bodyText(m *Model) string {
	cols, rows := m.termGeom(false)
	return strings.Join(stripAll(m.termBody(cols, rows)), "\n")
}

func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestJumpPromptWalksBackAndForward(t *testing.T) {
	m, tab := markedSession(t, 30)
	for i := 0; i < 10; i++ { // the first few prompts are still on the live screen
		feed(m, "alt+k")
	}
	if tab.scroll == 0 {
		t.Fatal("alt+k must scroll back once the prompt is off the live screen")
	}
	first := strings.SplitN(bodyText(m), "\n", 2)[0]
	var prev int
	if _, err := fmt.Sscanf(strings.TrimSpace(first), "prompt%d", &prev); err != nil {
		t.Fatalf("top row = %q, want a prompt line", first)
	}
	feed(m, "alt+k")
	var prev2 int
	fmt.Sscanf(strings.TrimSpace(strings.SplitN(bodyText(m), "\n", 2)[0]), "prompt%d", &prev2)
	if prev2 != prev-1 {
		t.Fatalf("second alt+k landed on prompt%02d, want prompt%02d", prev2, prev-1)
	}
	feed(m, "alt+j")
	var back int
	fmt.Sscanf(strings.TrimSpace(strings.SplitN(bodyText(m), "\n", 2)[0]), "prompt%d", &back)
	if back != prev {
		t.Fatalf("alt+j landed on prompt%02d, want prompt%02d", back, prev)
	}
	for i := 0; i < 40; i++ {
		feed(m, "alt+j")
	}
	if tab.scroll != 0 {
		t.Fatalf("alt+j past the last prompt must return to live, scroll=%d", tab.scroll)
	}
}

func TestJumpPromptStopsAtOldest(t *testing.T) {
	m, tab := markedSession(t, 12)
	for i := 0; i < 30; i++ {
		feed(m, "alt+k")
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.SplitN(bodyText(m), "\n", 2)[0]), "prompt00") {
		t.Fatalf("oldest prompt not at the top:\n%s", bodyText(m))
	}
	if m.toast == nil || !strings.Contains(m.toast.Detail, "no earlier prompt") {
		t.Fatalf("toast = %+v", m.toast)
	}
	_ = tab
}

func TestJumpPromptWithoutMarksSaysHow(t *testing.T) {
	m := newEditor(t)
	feed(m, "ctrl+t")
	m.activeTermTab().screen.Feed([]byte("$ ls\r\n"))
	feed(m, "alt+k")
	if m.toast == nil || !strings.Contains(m.toast.Detail, "OSC 133") {
		t.Fatalf("toast = %+v, want the enable-integration hint", m.toast)
	}
}

func TestFailedCommandGetsGutterMark(t *testing.T) {
	m, tab := markedSession(t, 4, 1)
	// 4 prompts and the cursor row fit on screen: live view shows them
	body := stripAll(m.termBodyOf(tab, 60, 6, false))
	var marked []string
	for _, l := range body {
		if strings.HasSuffix(strings.TrimRight(l, " "), "✗") {
			marked = append(marked, strings.TrimSpace(l))
		}
	}
	if len(marked) != 1 || !strings.HasPrefix(marked[0], "prompt01") {
		t.Fatalf("✗ rows = %q, want only prompt01\n%s", marked, strings.Join(body, "\n"))
	}
}

func TestFailedMarkFollowsScrollback(t *testing.T) {
	m, tab := markedSession(t, 40, 3)
	m.Resize(100, 30)
	tab.scroll = 1 << 10 // as far back as it goes
	cols, rows := m.termGeom(false)
	body := stripAll(m.termBodyOf(tab, cols, rows, false))
	found := false
	for _, l := range body {
		if strings.HasPrefix(strings.TrimSpace(l), "prompt03") {
			found = strings.HasSuffix(strings.TrimRight(l, " "), "✗")
		}
	}
	if !found {
		t.Fatalf("prompt03 should carry ✗ in scrollback:\n%s", strings.Join(body, "\n"))
	}
}

func TestNewTabAndSplitOpenInReportedFolder(t *testing.T) {
	m := liveEditor(t)
	feed(m, "ctrl+t")
	tab := m.activeTermTab()
	if tab == nil || tab.sess == nil {
		t.Skip("no session started")
	}
	dir := t.TempDir()
	tab.screen.Feed([]byte("\x1b]7;file://" + dir + "\x07"))
	feed(m, "alt+n")
	if got := m.activeTermTab().dir; got != dir {
		t.Fatalf("alt+n tab dir = %q, want %q", got, dir)
	}
	m.activeTerm = m.termIndex(tab.id)
	feed(m, "alt+s")
	if got := m.activeTermTab().dir; got != dir {
		t.Fatalf("split dir = %q, want %q", got, dir)
	}
}

func TestReportedFolderThatVanishedFallsBackToTabDir(t *testing.T) {
	m := liveEditor(t)
	feed(m, "ctrl+t")
	tab := m.activeTermTab()
	if tab == nil || tab.sess == nil {
		t.Skip("no session started")
	}
	gone := filepath.Join(t.TempDir(), "gone")
	tab.screen.Feed([]byte("\x1b]7;file://" + gone + "\x07"))
	if got := tab.workDir(); got != tab.dir {
		t.Fatalf("workDir = %q, want the tab's own %q", got, tab.dir)
	}
}

func TestSendSelectionToTerminal(t *testing.T) {
	m := liveApp(t)
	feed(m, "ctrl+t") // start the shell, then back to the buffer
	tab := m.activeTermTab()
	if tab == nil || tab.sess == nil {
		t.Skip("no session started")
	}
	feed(m, "ctrl+t", "ctrl+t") // blur, close
	m.bufFocus = true
	feed(m, "v", "$", "alt+enter")
	if !m.drawerOpen || !m.termFocus {
		t.Fatalf("send must open and focus the terminal (open=%v focus=%v)", m.drawerOpen, m.termFocus)
	}
	if msg := m.active().Message(); !strings.Contains(msg, "sent") {
		t.Fatalf("message = %q", msg)
	}
	// the shell's tty echoes what it received
	deadline := time.After(5 * time.Second)
	var seen strings.Builder
	for !strings.Contains(seen.String(), "package main") {
		select {
		case msg := <-m.termMsgs: // the pump delivers the shell's echo here
			if msg.kind == termMsgOut && msg.tab == tab.id {
				seen.Write(msg.chunk)
			}
		case <-deadline:
			t.Fatalf("terminal never received the selection; saw %q", seen.String())
		}
	}
}

func TestSendCommandWithoutSelectionSendsLine(t *testing.T) {
	m := liveApp(t)
	feed(m, "ctrl+t")
	if tab := m.activeTermTab(); tab == nil || tab.sess == nil {
		t.Skip("no session started")
	}
	feed(m, "ctrl+t", "ctrl+t")
	m.bufFocus = true
	typeKeys(m, ":send")
	feed(m, "enter")
	if !m.termFocus {
		t.Fatal(":send must focus the terminal")
	}
}

func TestSendRefusesWithoutLiveTerminal(t *testing.T) {
	m := newEditor(t) // no environment: the shell never starts
	if msg := m.sendToTerm("ls"); !strings.Contains(msg, "no live terminal") {
		t.Fatalf("msg = %q", msg)
	}
	if msg := m.sendToTerm(""); msg != "nothing to send" {
		t.Fatalf("msg = %q", msg)
	}
}

// liveApp is liveEditor with repos/alpha/app.go open.
func liveApp(t *testing.T) *Model {
	t.Helper()
	m := liveEditor(t)
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.active() == nil {
		t.Fatal("app.go did not open")
	}
	return m
}
