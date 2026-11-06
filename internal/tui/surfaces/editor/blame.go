package editor

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Inline blame (F-143): the cursor line shows, dimmed after its text,
// who last changed it and when. git blame runs once per file on a
// goroutine, the result is cached on the tab, and a save, reload or
// commit drops it. Rendering never waits for git.

// blameState is one tab's cached blame.
type blameState struct {
	gen     int // bumped by invalidation; a result from an older gen is dropped
	started bool
	loaded  bool
	lines   []gitcore.BlameLine // empty when git could not blame the file (untracked)
}

// blameResult is a finished git blame, posted back to the UI goroutine.
type blameResult struct {
	path  string
	gen   int
	lines []gitcore.BlameLine
}

// blameEnabled is the effective setting: a session :set blame|noblame
// beats editor.blame.
func (m *Model) blameEnabled(p Prefs) bool {
	if m.blameOverride != nil {
		return *m.blameOverride
	}
	return p.Blame
}

// blameCommand handles :set blame and :set noblame (this session only).
func (m *Model) blameCommand(cmd string) (string, bool) {
	var on bool
	switch strings.TrimSpace(cmd) {
	case "set blame":
		on = true
	case "set noblame":
		on = false
	default:
		return "", false
	}
	m.blameOverride = &on
	if on {
		return "blame: on", true
	}
	return "blame: off", true
}

// invalidate drops the cached blame so the next render asks git again.
func (s *blameState) invalidate() {
	*s = blameState{gen: s.gen + 1}
}

// invalidateBlame drops the blame of the tab holding path.
func (m *Model) invalidateBlame(path string) {
	for _, t := range m.bufs {
		if t.path == path {
			t.blame.invalidate()
		}
	}
}

// startBlame launches git blame for t unless it already ran for this
// generation. Called from render; the work is off the UI goroutine.
func (m *Model) startBlame(t *bufTab) {
	s := &t.blame
	if s.started || m.gitCLI == nil || m.termMsgs == nil {
		return
	}
	s.started = true
	root, rel, ok := m.repoRel(t.path)
	if !ok {
		s.loaded = true
		return
	}
	r, ch, path, gen := m.gitCLI, m.termMsgs, t.path, s.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		lines, _ := gitcore.Blame(ctx, r, root, rel) // untracked or unreadable: no annotation
		ch <- teaMsg{kind: blameMsgDone, blame: &blameResult{path: path, gen: gen, lines: lines}}
	}()
}

// applyBlame stores a finished blame on its tab.
func (m *Model) applyBlame(r *blameResult) {
	if r == nil {
		return
	}
	for _, t := range m.bufs {
		if t.path == r.path && t.blame.gen == r.gen {
			t.blame.lines, t.blame.loaded = r.lines, true
		}
	}
}

// blameFor finds the commit of buffer line l from blame taken at the
// last save. Edits since then shift lines, so the unchanged head and
// tail of the file map straight across; a line inside the edited middle
// is "uncommitted" when the line count held, otherwise unknown.
func blameFor(s *blameState, cur []string, l int) (gitcore.BlameLine, bool) {
	if !s.loaded || len(s.lines) == 0 || l < 0 || l >= len(cur) {
		return gitcore.BlameLine{}, false
	}
	old := s.lines
	p := 0
	for p < len(old) && p < len(cur) && old[p].Text == cur[p] {
		p++
	}
	q := 0
	for q < len(old)-p && q < len(cur)-p && old[len(old)-1-q].Text == cur[len(cur)-1-q] {
		q++
	}
	switch {
	case l < p:
		return old[l], true
	case l >= len(cur)-q:
		return old[l-(len(cur)-len(old))], true
	case len(cur) == len(old), p+q == len(old):
		return gitcore.BlameLine{Uncommitted: true}, true // edited in place, or inserted
	}
	return gitcore.BlameLine{}, false
}

// blameText is the annotation for the cursor line, "" when there is none.
func (m *Model) blameText(t *bufTab, l int) string {
	b, ok := blameFor(&t.blame, t.ed.Buffer().Lines(), l)
	if !ok {
		return ""
	}
	if b.Uncommitted {
		return "not committed yet"
	}
	return b.Author + ", " + relativeAge(b.When, m.blameNow()) + " · " + b.Subject
}

func (m *Model) blameNow() time.Time {
	if m.nowFn != nil {
		return m.nowFn()
	}
	return time.Now()
}

// blameSuffix is the dimmed annotation that fits in room cells after a
// line (two spaces apart), or "" when it does not fit.
func blameSuffix(text string, room int) string {
	const gap, minShown = 2, 12
	room -= gap
	if text == "" || room < minShown {
		return ""
	}
	if ansi.Width(text) > room {
		text = ansi.Slice(text, 0, room-1) + "…"
	}
	return strings.Repeat(" ", gap) + theme.TextDim().Render(text)
}

// relativeAge words how long ago at was, like git's "2 hours ago".
func relativeAge(at, now time.Time) string {
	d := now.Sub(at)
	plural := func(n int, unit string) string {
		s := strconv.Itoa(n) + " " + unit
		if n != 1 {
			s += "s"
		}
		return s + " ago"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour")
	case d < 14*24*time.Hour:
		return plural(int(d/(24*time.Hour)), "day")
	case d < 60*24*time.Hour:
		return plural(int(d/(7*24*time.Hour)), "week")
	case d < 365*24*time.Hour:
		return plural(int(d/(30*24*time.Hour)), "month")
	}
	return plural(int(d/(365*24*time.Hour)), "year")
}
