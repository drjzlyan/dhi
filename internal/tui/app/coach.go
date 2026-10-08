package app

import (
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/tutorial"
)

// The tutorial coach (F-048): a four-row strip under the live UI. The UI
// above stays fully usable — the strip only tells you what to try and
// advances when the shell sees you do it.
const (
	coachHeight = 4
	// Chords for the two things the user may need to do mid-lesson. They
	// avoid ctrl+n/ctrl+e/ctrl+x, which text fields and finders use.
	keyCoachNext = "ctrl+]"
	keyCoachEnd  = "ctrl+\\"
)

type coach struct {
	t   tutorial.Tutorial
	idx int
}

// TutorialHooks lets the host persist progress.
type TutorialHooks struct {
	// Done reports lessons already completed (palette shows a check).
	Done func(slug string) bool
	// OnEnd is called when a lesson ends; finished is true when the user
	// reached the last step (rather than ending it early).
	OnEnd func(slug string, finished bool)
}

// SetTutorials registers the lessons offered in the palette.
func (a *App) SetTutorials(ts []tutorial.Tutorial, h TutorialHooks) {
	a.tutorials, a.tutHooks = ts, h
}

func (a *App) coachActive() bool { return a.coach != nil }

// StartTutorial begins a lesson over the live UI. A running lesson is
// replaced (and reported as ended early).
func (a *App) StartTutorial(t tutorial.Tutorial) {
	if a.coach != nil {
		a.endCoach(false)
	}
	a.coach = &coach{t: t}
	a.resizeAll()
	a.observe("") // a first step that is already satisfied (e.g. view:x while on x)
}

// resizeAll re-lays the surfaces after the body height changed.
func (a *App) resizeAll() {
	for _, s := range a.surfaces {
		s.Resize(a.bodyWidth(), a.bodyHeight())
	}
	if a.gate != nil {
		a.gate.Resize(a.bodyWidth(), a.bodyHeight())
	}
}

func (a *App) endCoach(finished bool) {
	c := a.coach
	if c == nil {
		return
	}
	a.coach = nil
	a.resizeAll()
	if a.tutHooks.OnEnd != nil {
		a.tutHooks.OnEnd(c.t.Slug, finished)
	}
}

// coachAdvance moves to the next step, or finishes the lesson.
func (a *App) coachAdvance() {
	c := a.coach
	if c == nil {
		return
	}
	c.idx++
	if c.idx >= len(c.t.Steps) {
		a.endCoach(true)
		return
	}
	a.observe("") // the next step may already hold (e.g. already on that view)
}

// observe is the shell's event feed for lessons: "view:<id>", "palette",
// "help". The empty event re-checks the current step against the live state.
func (a *App) observe(event string) {
	c := a.coach
	if c == nil || a.gateActive() {
		return
	}
	step := c.t.Steps[c.idx]
	switch {
	case step.Await == "":
		return
	case event != "" && step.Await == event:
		a.coachAdvance()
	case event == "" && step.Await == "view:"+a.Active().Meta().ID:
		a.coachAdvance()
	}
}

// coachKey handles the two lesson chords; it reports whether it took the key.
func (a *App) coachKey(key string) bool {
	switch key {
	case keyCoachNext:
		a.coachAdvance()
		return true
	case keyCoachEnd:
		a.endCoach(false)
		return true
	}
	return false
}

// coachView renders exactly coachHeight rows.
func (a *App) coachView() string {
	c := a.coach
	step := c.t.Steps[c.idx]
	w := max(a.width, 20)

	head := theme.Brand().Render(theme.GlyphSpark+" "+kit.ClipEllipsis(c.t.Name, max(w/2-8, 8))) +
		theme.TextDim().Render("  "+itoa(c.idx+1)+"/"+itoa(len(c.t.Steps))+" · ") +
		theme.TextStyle().Render(kit.ClipEllipsis(step.Title, max(w/2-8, 8)))
	body := kit.WrapWords(step.Body, w-2)
	for len(body) < 2 {
		body = append(body, "")
	}
	if len(body) > 2 {
		body[1] = kit.ClipEllipsis(strings.Join(body[1:], " "), w-2)
		body = body[:2]
	}
	hint := theme.Hint().Render(keyCoachNext + " " + nextVerb(step) + " · " + keyCoachEnd + " end lesson")
	if step.Await != "" {
		hint = theme.AccentText().Render("waiting for you…") + theme.Hint().Render("  "+keyCoachNext+" skip step · "+keyCoachEnd+" end lesson")
	}
	return strings.Join([]string{head,
		theme.TextStyle().Render(body[0]), theme.TextStyle().Render(body[1]), hint}, "\n")
}

func nextVerb(s tutorial.Step) string {
	if s.Await == "" {
		return "continue"
	}
	return "skip step"
}

func itoa(n int) string { return strconv.Itoa(n) }

// coachCommands are the palette entries for lessons.
func (a *App) coachCommands(add func(group, title, hint string, run func() tea.Cmd)) {
	if a.coach != nil {
		add("Tutorial", "End this lesson", keyCoachEnd, func() tea.Cmd { a.endCoach(false); return nil })
	}
	for _, t := range a.tutorials {
		t := t
		title := t.Name
		if a.tutHooks.Done != nil && a.tutHooks.Done(t.Slug) {
			title += " ✓"
		}
		add("Tutorial", title, "", func() tea.Cmd { a.StartTutorial(t); return nil })
	}
}
