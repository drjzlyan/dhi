package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lhSaved(t *testing.T, m *Model, text string) {
	t.Helper()
	e := m.active()
	e.Buffer().SetText(text)
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	m.histStore().Wait()
}

// F-155: :w snapshots; :localhistory lists newest first with +/- counts,
// enter compares read-only, r restores as one undo step, unwritten.
func TestLocalHistorySaveCompareRestore(t *testing.T) {
	m, path := openApp(t)
	lhSaved(t, m, "one\ntwo\n")
	lhSaved(t, m, "one\nTWO\nthree\n")
	lhSaved(t, m, "one\nTWO\nthree\n") // identical save: no new snapshot
	m.active().Buffer().SetText("final\n")

	if msg, ok := m.fileCommand(m.active(), "localhistory"); !ok || msg != "" || m.lh == nil {
		t.Fatalf("open: %q %v", msg, ok)
	}
	if len(m.lh.list) != 2 {
		t.Fatalf("want 2 snapshots, got %d", len(m.lh.list))
	}
	view := plainView(m)
	for _, want := range []string{"local history · app.go", "vs buffer"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}

	feed(m, "enter")
	view = plainView(m)
	for _, want := range []string{"snapshot → buffer", "-one", "-TWO", "-three", "+final"} {
		if !strings.Contains(view, want) {
			t.Fatalf("compare missing %q:\n%s", want, view)
		}
	}
	feed(m, "esc")
	if m.lh == nil || m.lh.compare != nil {
		t.Fatal("esc should return to the list")
	}

	feed(m, "j", "r") // the older snapshot
	if m.lh != nil {
		t.Fatal("restore should close the picker")
	}
	e := m.active()
	if e.Buffer().Text() != "one\ntwo\n" || !e.Buffer().Dirty() {
		t.Fatalf("buffer = %q dirty=%v", e.Buffer().Text(), e.Buffer().Dirty())
	}
	if got := diskText(t, path); got != "one\nTWO\nthree\n" {
		t.Fatalf("restore wrote to disk: %q", got)
	}
	e.Buffer().Undo()
	if e.Buffer().Text() != "final\n" {
		t.Fatalf("restore was not one undo step: %q", e.Buffer().Text())
	}
}

func TestLocalHistoryCountsAgainstBuffer(t *testing.T) {
	a, r := lineCounts([]string{"a", "b", "c"}, []string{"a", "X", "c", "d"})
	if a != 2 || r != 1 {
		t.Fatalf("+%d -%d", a, r)
	}
}

func TestLocalHistoryAutosaveSnapshots(t *testing.T) {
	m, _, clk := autosaveEditor(t, "delay")
	feed(m, "A", "!", "esc")
	*clk = clk.Add(autosaveDelay + 1)
	m.Update(autosaveTickMsg{})
	m.histStore().Wait()
	if msg := m.openLocalHistory(); msg != "" || len(m.lh.list) != 1 {
		t.Fatalf("autosave left no snapshot: %q", msg)
	}
}

func TestLocalHistoryEmptyWritesNothing(t *testing.T) {
	m, _ := openApp(t)
	if msg := m.openLocalHistory(); !strings.Contains(msg, "no saved snapshots") {
		t.Fatalf("msg = %q", msg)
	}
	if _, err := os.Stat(filepath.Join(m.ws.Root, ".dhi", "history")); err == nil {
		t.Fatal("opening the list must not write")
	}
}

func TestUnifiedLinesContextAndGaps(t *testing.T) {
	var old []string
	for i := 0; i < 30; i++ {
		old = append(old, "l"+string(rune('a'+i%26))+string(rune('0'+i/10)))
	}
	cur := append([]string(nil), old...)
	cur[2], cur[25] = "X", "Y"
	got := strings.Join(unifiedLines(old, cur), "\n")
	for _, want := range []string{"-lc0\n+X", "-lz2\n+Y", "@@ line 23 @@"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, " lm1") {
		t.Fatalf("far-away lines leaked in as context:\n%s", got)
	}
}
