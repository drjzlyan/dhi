package reviewer

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/jsonl"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- run transcript in the reviewer (F-014 deferred) ----
//
// The DIFF section can be swapped for the newest run transcript of the
// task the review is bound to (a `task/<slug>` branch), so a review can
// read the agent's work alongside the diff. The transcript is the same
// persisted JSONL the run-replay pane reads; a missing file renders a
// named refusal, never fake data.

// traceEvent is one persisted transcript record (F-013 step 4).
type traceEvent struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// reviewTaskSlug extracts the task slug from a review bound to a
// `task/<slug>` branch (Head for branch reviews, HeadBranch for PRs).
func reviewTaskSlug(r review.Review) string {
	for _, b := range []string{r.Target.Head, r.Target.HeadBranch} {
		if strings.HasPrefix(b, "task/") {
			return strings.TrimPrefix(b, "task/")
		}
	}
	return ""
}

// toggleTranscript opens (or closes) the run transcript for the open
// review's task. A missing binding/run degrades with a named error.
func (m *Model) toggleTranscript() {
	if m.transcriptOpen {
		m.transcriptOpen = false
		m.transcriptScroll = 0
		return
	}
	r, ok := m.openReview()
	if !ok {
		m.opErr = "no review open"
		return
	}
	slug := reviewTaskSlug(r)
	if slug == "" {
		m.opErr = "review is not bound to a task branch — no run transcript"
		return
	}
	if m.taskStore == nil {
		m.opErr = "task store unavailable"
		return
	}
	t, found := m.taskStore.Get(slug)
	if !found {
		m.opErr = "task " + slug + " not found"
		return
	}
	run, has := t.NewestRun()
	if !has {
		m.opErr = "task " + slug + " has no runs recorded"
		return
	}
	m.transcriptTitle, m.transcriptLines = renderTrace(run, m.transcriptWidth())
	m.transcriptOpen = true
	m.transcriptScroll = 0
}

func (m *Model) transcriptWidth() int { return maxInt(m.width-12, 30) }

// renderTrace folds one run's transcript into a title + wrapped lines.
func renderTrace(run tasks.Run, width int) (string, []string) {
	model := run.Model
	if model == "" {
		model = "(default)"
	}
	title := fmt.Sprintf("run %s · %s · %s · %s", run.ID, run.Runtime, model, run.Status)
	if run.Transcript == "" {
		return title, []string{theme.DangerText().Render("no transcript recorded for this run")}
	}
	evs, err := jsonl.ReadAll[traceEvent](run.Transcript)
	if err != nil {
		return title, []string{theme.DangerText().Render("transcript unavailable at " + run.Transcript)}
	}
	if len(evs) == 0 {
		return title, []string{theme.TextDim().Render("(transcript empty)")}
	}
	var out []string
	for _, ev := range evs {
		glyph, style := "·", theme.TextDim()
		switch ev.Kind {
		case "command":
			glyph, style = "→", theme.Hint()
		case "error":
			glyph, style = "✗", theme.DangerText()
		case "final":
			glyph, style = "■", theme.SuccessText()
		}
		for i, ln := range kit.WrapWords(ev.Detail, maxInt(width-4, 20)) {
			prefix := "  "
			if i == 0 {
				prefix = glyph + " "
			}
			out = append(out, style.Render(prefix+ln))
		}
	}
	return title, out
}

// renderTranscript draws the transcript window for the DIFF pane.
func (m *Model) renderTranscript(w, h int) string {
	rows := maxInt(h-3, 3)
	max := len(m.transcriptLines) - rows
	if max < 0 {
		max = 0
	}
	if m.transcriptScroll > max {
		m.transcriptScroll = max
	}
	end := m.transcriptScroll + rows
	if end > len(m.transcriptLines) {
		end = len(m.transcriptLines)
	}
	head := theme.TextDim().Render(m.transcriptTitle)
	hint := theme.Hint().Render(fmt.Sprintf("  %d/%d · j/k scroll · esc back",
		minInt(end, len(m.transcriptLines)), len(m.transcriptLines)))
	out := []string{head, hint}
	out = append(out, m.transcriptLines[m.transcriptScroll:end]...)
	return strings.Join(out, "\n")
}

// transcriptKey handles the transcript view's scroll/close keys; the
// view swallows everything else (like the thread view).
func (m *Model) transcriptKey(key string) bool {
	switch key {
	case "j", "down":
		if m.transcriptScroll < len(m.transcriptLines)-1 {
			m.transcriptScroll++
		}
	case "k", "up":
		if m.transcriptScroll > 0 {
			m.transcriptScroll--
		}
	case "g":
		m.transcriptScroll = 0
	case "G":
		m.transcriptScroll = maxInt(len(m.transcriptLines)-1, 0)
	case "esc", "T":
		m.transcriptOpen = false
		m.transcriptScroll = 0
	}
	return true
}
