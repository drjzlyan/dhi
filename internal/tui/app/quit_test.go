package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

type dirtyStub struct {
	stubSurface
	names   []string
	saveErr error
	saved   int
}

func (d *dirtyStub) Unsaved() []string { return d.names }
func (d *dirtyStub) SaveAll() error {
	d.saved++
	if d.saveErr == nil {
		d.names = nil
	}
	return d.saveErr
}

func newDirtyApp(t *testing.T, names ...string) (*App, *dirtyStub) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	d := &dirtyStub{stubSurface: stubSurface{id: "editor", title: "Editor"}, names: names}
	a := New("test", d, &stubSurface{id: "other", title: "Other"})
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return a, d
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// F-115: quitting with unsaved buffers asks first.
func TestQuitWithNothingUnsavedQuits(t *testing.T) {
	a, _ := newDirtyApp(t)
	if _, cmd := a.Update(keyPress("ctrl+c")); !isQuit(cmd) {
		t.Fatal("clean quit must quit at once")
	}
}

func TestQuitGuardAsksAndCancels(t *testing.T) {
	a, _ := newDirtyApp(t, "proj/main.go")
	if _, cmd := a.Update(keyPress("ctrl+c")); isQuit(cmd) {
		t.Fatal("quit dropped unsaved work")
	}
	v := ansi.Strip(a.View().Content)
	if !strings.Contains(v, "Quit DHI?") || !strings.Contains(v, "proj/main.go") {
		t.Fatalf("guard not shown:\n%s", v)
	}
	if _, cmd := a.Update(keyPress("esc")); isQuit(cmd) || a.quitAsk != nil {
		t.Fatal("esc must cancel")
	}
	a.Update(keyPress("ctrl+q"))
	if _, cmd := a.Update(keyPress("d")); !isQuit(cmd) {
		t.Fatal("d must quit without saving")
	}
}

func TestQuitGuardSavesAll(t *testing.T) {
	a, d := newDirtyApp(t, "a.go")
	a.Update(keyPress("ctrl+c"))
	if _, cmd := a.Update(keyPress("s")); !isQuit(cmd) || d.saved != 1 {
		t.Fatalf("s: saved=%d", d.saved)
	}
}

func TestQuitGuardStaysOnSaveError(t *testing.T) {
	a, d := newDirtyApp(t, "a.go")
	d.saveErr = errors.New("a.go: file changed on disk")
	a.Update(keyPress("ctrl+c"))
	if _, cmd := a.Update(keyPress("s")); isQuit(cmd) {
		t.Fatal("quit although the save failed")
	}
	if v := ansi.Strip(a.View().Content); !strings.Contains(v, "changed on disk") {
		t.Fatalf("error not shown:\n%s", v)
	}
}

type bgMsg struct{}

// F-115/ADR-0045: a background message reaches surfaces that are not on
// screen, so their listeners keep re-arming.
func TestBackgroundMessagesReachEverySurface(t *testing.T) {
	a, stubs := newTestApp(t)
	before := stubs[2].updates
	a.Update(bgMsg{})
	if stubs[2].updates != before+1 || stubs[1].updates == 0 {
		t.Fatalf("inactive surfaces not updated: %d → %d", before, stubs[2].updates)
	}
}

// F-115: a view switch that no key made (a click, a cross-view jump) still
// arms the fade, so the view does not stay dimmed.
func TestNonKeySwitchArmsTransition(t *testing.T) {
	a, _ := newTestApp(t)
	old := theme.Motion
	theme.Motion = true
	t.Cleanup(func() { theme.Motion = old })
	a.selectSurface(2) // as OpenInEditor and tab clicks do
	_, cmd := a.Update(bgMsg{})
	if cmd == nil || !a.transArmed {
		t.Fatal("fade tick not armed")
	}
	for i := 0; i < transitionFrames+1; i++ {
		a.Update(transitionMsg{})
	}
	if a.transLeft != 0 {
		t.Fatalf("still dimmed: %d frames left", a.transLeft)
	}
}

func TestOSC9IsSafe(t *testing.T) {
	got := osc9("@ana replied", "done\x07\x1b]9;evil")
	if strings.Count(got, "\x07") != 1 || strings.Count(got, "\x1b") != 1 || !strings.Contains(got, "@ana replied — done") {
		t.Fatalf("%q", got)
	}
}
