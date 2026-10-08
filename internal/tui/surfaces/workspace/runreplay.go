package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/jsonl"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// replayEvent is one persisted transcript record (F-013 step 4): the
// same {kind,detail} shape saveTranscript writes.
type replayEvent struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// runReplay is the F-014 run-replay pane: a run's transcript JSONL
// rendered chronologically — progress text, tool commands, errors in
// danger color, the final usage line — word-wrapped and scrollable. A
// missing or pruned transcript renders the named refusal, never fake data.
type runReplay struct {
	run    tasks.Run
	readAt string // transcript path for display: workspace-relative when inside it
	events []replayEvent
	err    string   // transcript read failure, shown as the named refusal
	lines  []string // cached wrapped render, refreshed on geometry change
	scroll int
	width  int // geometry the cache was built for
	height int
}

// openReplay loads the transcript for r, tolerating an absent file.
// root (the workspace root, may be "") shortens the displayed path.
func openReplay(r tasks.Run, root string) *runReplay {
	rp := &runReplay{run: r, readAt: displayPath(r.Transcript, root)}
	if r.Transcript == "" {
		rp.err = "no transcript recorded for this run"
		return rp
	}
	if _, serr := os.Stat(r.Transcript); os.IsNotExist(serr) {
		rp.err = "transcript file missing"
		return rp
	} else if serr != nil {
		rp.err = serr.Error()
		return rp
	}
	evs, err := jsonl.ReadAll[replayEvent](r.Transcript)
	if err != nil {
		rp.err = err.Error()
		return rp
	}
	rp.events = evs
	if len(evs) == 0 {
		rp.err = "transcript empty — no events persisted"
	}
	return rp
}

// displayPath shows p relative to root when it lives inside it.
func displayPath(p, root string) string {
	if root == "" || p == "" {
		return p
	}
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

func (rp *runReplay) Key(key string, m *Model) bool {
	rp.refresh(m.replayWidth(), m.replayHeight())
	switch key {
	case "j", "down":
		if rp.scroll < rp.windowMax() {
			rp.scroll++
		}
		return true
	case "k", "up":
		if rp.scroll > 0 {
			rp.scroll--
		}
		return true
	case "G":
		rp.scroll = rp.windowMax()
		return true
	case "g":
		rp.scroll = 0
		return true
	case "esc":
		m.replay = nil
		return true
	default:
		return true // modal: swallowed until esc / section switch
	}
}

// replayWidth/Height bound the transcript window inside the pane.
func (m *Model) replayWidth() int {
	w := m.width - railWidth
	if w < 40 {
		w = 40
	}
	return w - 8
}

func (m *Model) replayHeight() int {
	return maxInt(m.height-6, 8)
}

// refresh rebuilds the wrapped render when geometry changed and keeps
// the scroll offset inside the window.
func (rp *runReplay) refresh(width, height int) {
	if width != rp.width || height != rp.height {
		rp.lines = rp.render(width)
		rp.width, rp.height = width, height
	}
	max := rp.windowMax()
	if rp.scroll > max {
		rp.scroll = max
	}
	if rp.scroll < 0 {
		rp.scroll = 0
	}
}

func (rp *runReplay) windowMax() int {
	return maxInt(len(rp.lines)-rp.height-2, 0)
}

// render converts the transcript to styled, wrapped lines (F-014 §Part B).
func (rp *runReplay) render(width int) []string {
	var out []string
	if rp.err != "" {
		out = append(out, theme.DangerText().Render("transcript unavailable at "+rp.readAt))
		out = append(out, theme.TextDim().Render("  "+rp.err))
		return out
	}
	for _, ev := range rp.events {
		detail := strings.TrimSpace(ev.Detail)
		if detail == "" {
			continue
		}
		var line string
		switch ev.Kind {
		case "command":
			line = theme.Hint().Render("❯ ") + detail
		case "error":
			line = theme.DangerText().Render(detail)
		case "final":
			line = theme.Hint().Render("▸ " + detail)
		default:
			line = detail // progress text and anything unknown
		}
		out = append(out, kit.WrapWords(line, width)...)
	}
	if len(out) == 0 {
		out = append(out, theme.TextDim().Render("(empty transcript — no events persisted)"))
	}
	return out
}

// body returns the replay header plus the scrolled transcript window.
// Keys are advertised on the pane's HintBar (F-026 P3), not in-body.
func (m *Model) replayBody() string {
	rp := m.replay
	if rp == nil {
		return ""
	}
	width, height := m.replayWidth(), m.replayHeight()
	rp.refresh(width, height)

	header := m.replayHeader()
	from := minInt(rp.scroll, maxInt(len(rp.lines)-height, 0))
	to := minInt(from+height, len(rp.lines))
	var window []string
	window = append(window, rp.lines[from:to]...)
	lines := append([]string{header}, window...)
	return strings.Join(lines, "\n")
}

// replayHeader summarises the run above its transcript.
func (m *Model) replayHeader() string {
	r := m.replay.run
	cost := "-"
	if r.HasCost {
		cost = fmtCost(r.CostUSD)
	}
	status := ""
	if r.Status != "" {
		status = m.runStyle(r.Status)
	}
	runtime := r.Runtime
	if runtime == "" {
		runtime = "cli:?"
	}
	model := r.Model
	if model == "" {
		model = "-"
	}
	return theme.Hint().Render(
		"replay " + r.ID + " · " + runtime + "/" + model + " · " + status +
			" · " + tasks.DurationText(runMs(r)) + " · " + cost)
}

func runMs(r tasks.Run) int64 {
	if r.Finished.Before(r.Started) {
		return 0
	}
	return r.Finished.Sub(r.Started).Milliseconds()
}

func fmtCost(v float64) string {
	s := strings.TrimRight(fmt.Sprintf("%.4f", v), "0")
	s = strings.TrimRight(s, ".")
	return "$" + s
}

// runStyle tints a run status for the replay/INSPECT column; the empty
// string renders bare.
func (m *Model) runStyle(s tasks.RunStatus) string {
	switch s {
	case tasks.RunOK:
		return theme.SuccessText().Render(string(s))
	case tasks.RunError:
		return theme.DangerText().Render(string(s))
	case tasks.RunTimeout:
		return theme.TextDim().Render(string(s))
	}
	return ""
}

// wrap was the rune hard-break; the shared kit.WrapWords (word-boundary
// wrap, F-026 P3) replaced it.
