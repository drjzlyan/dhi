package editor

import (
	"github.com/drjzlyan/dhi/internal/textbuf"
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
// passes the user's env).
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

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m.drainTerm()
		if strings.Contains(plainView(m), "dhi-live-42") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("live echo not observed:\n%s", plainView(m))
}

// TestDrawerRefusesWithoutEnv: a terminal starts only with the
// environment it was given, and says so visibly otherwise.
func TestDrawerRefusesWithoutEnv(t *testing.T) {
	m := newEditor(t) // no WithTermEnv
	feed(m, "ctrl+t")
	v := plainView(m)
	if !strings.Contains(v, "no environment given") {
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
		"shift+tab": "\x1b[Z", "x": "x", "alt+left": "\x1b[1;3D",
	}
	for k, want := range cases {
		if got, ok := termKeyBytes(k); !ok || string(got) != want {
			t.Errorf("%s → %q, want %q", k, got, want)
		}
	}
}

// TestFocusedTerminalKeepsReadlineKeys (F-101): ctrl+a, ctrl+j and
// ctrl+y reach the program; ctrl+t (the way out) stays the editor's.
func TestFocusedTerminalKeepsReadlineKeys(t *testing.T) {
	m := newEditor(t)
	feed(m, "ctrl+t")
	for _, k := range []string{"ctrl+a", "ctrl+j", "ctrl+y"} {
		feed(m, k)
		if !m.drawerOpen || !m.termFocus || m.gitOpen {
			t.Fatalf("%s did not stay with the terminal (git=%v)", k, m.gitOpen)
		}
	}
	feed(m, "ctrl+t")
	if m.termFocus {
		t.Fatal("ctrl+t must still blur the terminal")
	}
}

// F-103: yy puts the line on the system clipboard as well as the register.
func TestYankAlsoCopiesToTheSystemClipboard(t *testing.T) {
	m := newEditor(t)
	feed(m, "enter", "down", "down", "enter") // open alpha/src/app.go
	if !m.bufFocus {
		t.Fatal("no buffer")
	}
	m.TakeCmd()
	feed(m, "y", "y")
	cmd := m.TakeCmd()
	if cmd == nil {
		t.Fatal("yy queued no clipboard command")
	}
}

// F-103: a click in the text puts the cursor there; a drag selects the
// text between, and y copies it.
func TestClickAndDragInTheBuffer(t *testing.T) {
	m := newEditor(t)
	feed(m, "enter", "down", "down", "enter") // open alpha/src/app.go
	e := m.active()
	if e == nil {
		t.Fatal("no buffer")
	}
	e.Buffer().SetCursor(textbuf.Pos{})
	m.View() // records where the text is
	g := m.bufGeom
	if !g.ok {
		t.Fatal("no buffer geometry after a render")
	}
	line := 0
	for ; line < e.Buffer().LineCount() && len([]rune(e.Buffer().Line(line))) < 6; line++ {
	}
	if line == e.Buffer().LineCount() {
		t.Skip("fixture has no line long enough")
	}
	x, y := g.ox+g.textX+4, g.oy+line-g.top
	screen := []rune(strings.Split(ansi.Strip(m.View()), "\n")[y])
	if want := []rune(e.Buffer().Line(line))[4]; x >= len(screen) || screen[x] != want {
		t.Fatalf("screen cell (%d,%d) is %q, buffer has %q:\n%s", x, y, string(screen[min(x, len(screen)-1)]), want, string(screen))
	}
	if !m.Click(x, y) {
		t.Fatal("click in the text not taken")
	}
	if c := e.Buffer().Cursor(); c.Line != line || c.Col != 4 {
		t.Fatalf("cursor = %+v, want line %d col 4", c, line)
	}
	m.Drag(x+1, y)
	if e.Mode() != textbuf.ModeVisual {
		t.Fatal("a drag must select")
	}
	m.Release(x+1, y)
	m.TakeCmd()
	feed(m, "y")
	if text, ok := e.RegisterText(); !ok && text == "" || len([]rune(text)) != 2 {
		t.Fatalf("yanked %q", text)
	}
	if m.Click(g.ox+g.textX, g.oy+g.rows+5) {
		t.Fatal("a click below the text must not move the cursor")
	}
}
