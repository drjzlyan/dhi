package app

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/tutorial"
)

// lesson uses the stub surfaces' ids (home, editor, trees).
func lesson() tutorial.Tutorial {
	return tutorial.Tutorial{Slug: "demo", Name: "Demo lesson", Description: "d", Steps: []tutorial.Step{
		{Title: "Open editor", Body: "Press 2.", Await: "view:editor"},
		{Title: "Back home", Body: "Press 1.", Await: "view:home"},
		{Title: "Palette", Body: "Press ctrl+p.", Await: "palette"},
		{Title: "Help", Body: "Press ?.", Await: "help"},
		{Title: "Read this", Body: "Then continue."},
	}}
}

type ended struct {
	slug     string
	finished bool
}

func coachApp(t *testing.T) (*App, *[]ended) {
	t.Helper()
	theme.MotionForTest(t, false)
	a, _ := newTestApp(t)
	var log []ended
	a.SetTutorials([]tutorial.Tutorial{lesson()}, TutorialHooks{
		Done:  func(s string) bool { return s == "demo" && len(log) > 0 && log[len(log)-1].finished },
		OnEnd: func(s string, f bool) { log = append(log, ended{s, f}) },
	})
	return a, &log
}

func step(a *App) int { return a.coach.idx }

func TestLessonAdvancesOnTheActionsItAwaits(t *testing.T) {
	a, log := coachApp(t)
	a.StartTutorial(lesson())
	if step(a) != 0 {
		t.Fatalf("start step = %d", step(a))
	}
	a.Update(keyPress("2"))
	if step(a) != 1 {
		t.Fatalf("after switching to the editor: step %d, want 1", step(a))
	}
	a.Update(keyPress("3")) // wrong view: no progress
	if step(a) != 1 {
		t.Fatalf("an unrelated view advanced the lesson to %d", step(a))
	}
	a.Update(keyPress("1"))
	if step(a) != 2 {
		t.Fatalf("step %d after going home", step(a))
	}
	a.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if step(a) != 3 || a.palette == nil {
		t.Fatalf("palette: step %d palette=%v", step(a), a.palette != nil)
	}
	a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a.Update(keyPress("?"))
	if step(a) != 4 {
		t.Fatalf("help: step %d", step(a))
	}
	a.Update(keyPress("?")) // close help
	a.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if a.coachActive() || len(*log) != 1 || (*log)[0] != (ended{"demo", true}) {
		t.Fatalf("finishing: active=%v log=%v", a.coachActive(), *log)
	}
}

func TestStripTakesRowsFromTheLiveUIAndGivesThemBack(t *testing.T) {
	a, _ := coachApp(t)
	before := a.bodyHeight()
	a.StartTutorial(lesson())
	if a.bodyHeight() != before-coachHeight {
		t.Fatalf("body %d → %d, want -%d", before, a.bodyHeight(), coachHeight)
	}
	lines := strings.Split(ansi.Strip(a.compose()), "\n")
	strip := strings.Join(lines[len(lines)-1-coachHeight:len(lines)-1], "\n")
	if !strings.Contains(strip, "Demo lesson") || !strings.Contains(strip, "ctrl+\\ end lesson") {
		t.Fatalf("the strip is not directly above the status line:\n%s", strings.Join(lines, "\n"))
	}
	a.Update(tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl})
	if a.bodyHeight() != before || a.coachActive() {
		t.Fatalf("not restored: body %d active=%v", a.bodyHeight(), a.coachActive())
	}
}

func TestEndingEarlyIsReportedAsUnfinished(t *testing.T) {
	a, log := coachApp(t)
	a.StartTutorial(lesson())
	a.Update(tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl})
	if len(*log) != 1 || (*log)[0].finished {
		t.Fatalf("log = %v", *log)
	}
}

func TestStartingAnotherLessonEndsTheRunningOneEarly(t *testing.T) {
	a, log := coachApp(t)
	a.StartTutorial(lesson())
	a.StartTutorial(lesson())
	if len(*log) != 1 || (*log)[0].finished || !a.coachActive() || step(a) != 0 {
		t.Fatalf("log=%v active=%v step=%d", *log, a.coachActive(), step(a))
	}
}

func TestAStepAlreadySatisfiedAdvancesAtOnce(t *testing.T) {
	a, _ := coachApp(t)
	a.Update(keyPress("2")) // already in the editor
	a.StartTutorial(lesson())
	if step(a) != 1 {
		t.Fatalf("step = %d; being on the awaited view should skip step 1", step(a))
	}
}

func TestStripFitsAndNamesTheStep(t *testing.T) {
	a, _ := coachApp(t)
	a.StartTutorial(lesson())
	rows := strings.Split(ansi.Strip(a.coachView()), "\n")
	if len(rows) != coachHeight {
		t.Fatalf("%d rows, want %d", len(rows), coachHeight)
	}
	for i, r := range rows {
		if n := len([]rune(r)); n > a.width {
			t.Errorf("row %d is %d wide in a %d terminal: %q", i, n, a.width, r)
		}
	}
	for _, want := range []string{"Demo lesson", "1/5", "Open editor", "Press 2.", "waiting for you", "ctrl+] skip step", "ctrl+\\ end lesson"} {
		if !strings.Contains(strings.Join(rows, "\n"), want) {
			t.Errorf("strip lacks %q:\n%s", want, strings.Join(rows, "\n"))
		}
	}
	a.coach.idx = 4 // the manual step
	if v := ansi.Strip(a.coachView()); !strings.Contains(v, "ctrl+] continue") || strings.Contains(v, "waiting") {
		t.Fatalf("manual step:\n%s", v)
	}
}

func TestLongStepTextStaysWithinTwoRows(t *testing.T) {
	a, _ := coachApp(t)
	long := strings.Repeat("a very long instruction ", 30)
	a.StartTutorial(tutorial.Tutorial{Slug: "l", Name: "L", Description: "d",
		Steps: []tutorial.Step{{Title: "T", Body: long}}})
	rows := strings.Split(ansi.Strip(a.coachView()), "\n")
	if len(rows) != coachHeight {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if len([]rune(r)) > a.width {
			t.Fatalf("overflow: %q", r)
		}
	}
}

func TestPaletteOffersLessonsMarksDoneOnesAndEndsTheRunningOne(t *testing.T) {
	a, log := coachApp(t)
	a.openPalette()
	found := false
	for _, it := range a.palette.Matches() {
		if it.Group == "Tutorial" && it.Title == "Demo lesson" {
			found = true
		}
		if it.Title == "End this lesson" {
			t.Fatal("'End this lesson' offered with no lesson running")
		}
	}
	if !found {
		t.Fatal("lesson missing from the palette")
	}
	a.palette = nil

	for _, r := range "demo lesson" {
		a.openPaletteIfClosed()
		a.Update(keyPress(string(r)))
	}
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.coachActive() {
		t.Fatalf("picking the entry should start the lesson")
	}

	a.palette = nil
	a.openPalette()
	var ends, done bool
	for _, it := range a.palette.Matches() {
		ends = ends || it.Title == "End this lesson"
		done = done || strings.HasSuffix(it.Title, "✓")
	}
	if !ends || done {
		t.Fatalf("running lesson: ends=%v done=%v", ends, done)
	}
	a.endCoach(true)
	a.palette = nil
	a.openPalette()
	for _, it := range a.palette.Matches() {
		done = done || strings.HasSuffix(it.Title, "✓")
	}
	if !done || len(*log) == 0 {
		t.Fatal("a completed lesson should show a check")
	}
}

func (a *App) openPaletteIfClosed() {
	if a.palette == nil {
		a.openPalette()
	}
}

func TestLessonDoesNotAdvanceBehindAGate(t *testing.T) {
	a, _ := coachApp(t)
	a.SetGate(&stubGate{})
	a.StartTutorial(lesson())
	a.observe("view:editor")
	if step(a) != 0 {
		t.Fatalf("lesson advanced under a gate: step %d", step(a))
	}
}

// emitterStub is a surface that reports actions, like the editor does.
type emitterStub struct {
	stubSurface
	emit func(string)
}

func (e *emitterStub) SetEmitter(fn func(string)) { e.emit = fn }

func TestLessonAdvancesOnARealActionASurfaceReports(t *testing.T) {
	theme.MotionForTest(t, false)
	theme.SwapForTest(t, theme.Dark())
	em := &emitterStub{stubSurface: stubSurface{id: "editor", title: "Editor"}}
	a := New("test", em)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if em.emit == nil {
		t.Fatal("the shell did not hand the surface an emitter")
	}
	a.StartTutorial(tutorial.Tutorial{Slug: "x", Name: "X", Description: "d", Steps: []tutorial.Step{
		{Title: "Save", Body: "Type :w.", Await: "do:editor.save"},
		{Title: "Read", Body: "Done."},
	}})
	em.emit("editor.open") // a different action: no progress
	if step(a) != 0 {
		t.Fatalf("an unrelated action advanced the lesson to %d", step(a))
	}
	em.emit("editor.save")
	if step(a) != 1 {
		t.Fatalf("the awaited action left the lesson on step %d", step(a))
	}
	em.emit("editor.save") // already past it: must not skip the reading step
	if step(a) != 1 {
		t.Fatalf("a repeated action skipped ahead to %d", step(a))
	}
}

func TestActionsWithoutALessonAreIgnored(t *testing.T) {
	em := &emitterStub{stubSurface: stubSurface{id: "editor", title: "Editor"}}
	a := New("test", em)
	em.emit("editor.save") // no coach running: must not panic
	if a.coach != nil {
		t.Fatal("an action started a lesson")
	}
}
