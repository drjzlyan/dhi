package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/trace"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/jsonl"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// F-074: a card's trace — every run and attempt, the thread with its tool
// lines and notices, comments and activity, in one timeline — for
// debugging what an agent actually did. Chat shows conversation only;
// this is where the rest lives.

// traceFilters are the kind sets `f` cycles through.
var traceFilters = []struct {
	name  string
	kinds map[string]bool
}{
	{"everything", nil},
	{"conversation", map[string]bool{trace.KindMessage: true, trace.KindReply: true, trace.KindAlert: true, trace.KindComment: true}},
	{"tools", map[string]bool{"command": true, "progress": true, trace.KindNotice: true}},
	{"problems", map[string]bool{"error": true, "stderr": true, trace.KindAlert: true, trace.KindRunEnd: true}},
	{"prompts", map[string]bool{"prompt": true, "prompt_text": true, "system_text": true, trace.KindRunStart: true}},
}

type tracePane struct {
	tr        trace.Trace
	run       int // index into tr.Runs; -1 = every run
	filter    int
	query     string
	searching bool
	scroll    int
	note      string // last export path or error
}

// openTrace builds the card's trace; focus picks a run ("" = all).
func (m *Model) openTrace(tk tasks.Task, focus string) {
	var thread []bus.Message
	if m.bus != nil && tk.ThreadChannel != "" {
		for _, msg := range m.bus.History(tk.ThreadChannel, 0) {
			if msg.ID == tk.ThreadID {
				thread = append(thread, msg)
				break
			}
		}
		if tk.ThreadID != 0 {
			thread = append(thread, m.bus.History(tk.ThreadChannel, tk.ThreadID)...)
		}
	}
	tr := trace.Build(trace.Input{Task: tk, Thread: thread, Read: func(p string) ([]trace.Record, error) {
		if _, err := os.Stat(p); err != nil {
			return nil, err // a pruned transcript is named, not shown as empty
		}
		return jsonl.ReadAll[trace.Record](p)
	}})
	tp := &tracePane{tr: tr, run: -1}
	for i, r := range tr.Runs {
		if r.ID == focus {
			tp.run = i
		}
	}
	m.trace = tp
}

func (tp *tracePane) runID() string {
	if tp.run < 0 || tp.run >= len(tp.tr.Runs) {
		return ""
	}
	return tp.tr.Runs[tp.run].ID
}

func (tp *tracePane) events() []trace.Event {
	return tp.tr.Filter(tp.runID(), traceFilters[tp.filter].kinds, tp.query)
}

func (m *Model) traceKey(key string) bool {
	tp := m.trace
	if tp.searching {
		switch key {
		case "esc":
			tp.searching, tp.query = false, ""
		case "enter":
			tp.searching = false
		case "backspace":
			if r := []rune(tp.query); len(r) > 0 {
				tp.query = string(r[:len(r)-1])
			}
		default:
			if r := []rune(key); len(r) == 1 && r[0] >= 32 {
				tp.query += key
			}
		}
		tp.scroll = 0
		return true
	}
	switch key {
	case "esc", "q":
		m.trace = nil
	case "j", "down":
		tp.scroll++
	case "k", "up":
		tp.scroll = maxInt(tp.scroll-1, 0)
	case "ctrl+d", "pgdown":
		tp.scroll += 10
	case "ctrl+u", "pgup":
		tp.scroll = maxInt(tp.scroll-10, 0)
	case "g":
		tp.scroll = 0
	case "G":
		tp.scroll = 1 << 20
	case "n", "l", "right":
		if tp.run < len(tp.tr.Runs)-1 {
			tp.run++
			tp.scroll = 0
		}
	case "p", "h", "left":
		if tp.run > -1 {
			tp.run--
			tp.scroll = 0
		}
	case "f":
		tp.filter = (tp.filter + 1) % len(traceFilters)
		tp.scroll = 0
	case "/":
		tp.searching = true
	case "x":
		tp.note = m.exportTrace(tp)
	}
	return true // modal until esc
}

// exportTrace writes the visible events as Markdown and JSONL under
// .dhi/traces/ and returns a note naming the files.
func (m *Model) exportTrace(tp *tracePane) string {
	dir := filepath.Join(m.wsRoot(), ".dhi", "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "export failed: " + err.Error()
	}
	base := filepath.Join(dir, tp.tr.Task.Slug+"-"+m.now().Format("20060102-150405"))
	evs := tp.events()
	for _, out := range []struct {
		ext   string
		write func(*os.File) error
	}{
		{".md", func(f *os.File) error { return tp.tr.WriteMarkdown(f, evs) }},
		{".jsonl", func(f *os.File) error { return tp.tr.WriteJSONL(f, evs) }},
	} {
		f, err := os.Create(base + out.ext)
		if err != nil {
			return "export failed: " + err.Error()
		}
		err = out.write(f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "export failed: " + err.Error()
		}
	}
	return "exported " + displayPath(base, m.wsRoot()) + ".md / .jsonl"
}

// traceBody renders the header (card, run selector, filter) over the
// scrolled timeline.
func (m *Model) traceBody(w, h int) string {
	tp := m.trace
	tk := tp.tr.Task
	head := []string{
		theme.SectionHeader().Render("TRACE") + "  " + theme.TextStyle().Bold(true).Render(tk.Slug) +
			theme.TextDim().Render("  "+kit.ClipEllipsis(tk.Title, maxInt(w-20-len(tk.Slug), 8))),
		m.traceRunStrip(tp, w),
	}
	status := theme.TextDim().Render("filter ") + theme.AccentText().Render(traceFilters[tp.filter].name)
	switch {
	case tp.searching:
		status += "  " + theme.TabActive().Render("/"+tp.query) + "▏"
	case tp.query != "":
		status += theme.TextDim().Render("  search \"" + tp.query + "\"")
	}
	if len(tp.tr.Missing) > 0 {
		status += theme.WarningText().Render("  transcript missing: " + strings.Join(tp.tr.Missing, ", "))
	}
	if tp.note != "" {
		status += "  " + theme.SuccessText().Render(tp.note)
	}
	head = append(head, kit.ClipEllipsis(status, w), "")

	var body []string
	evs := tp.events()
	if len(evs) == 0 {
		body = append(body, theme.TextDim().Render("  nothing recorded for this view"))
	}
	var prevRun string
	for _, e := range evs {
		if e.Run != "" && e.Run != prevRun && tp.runID() == "" && e.Kind == trace.KindRunStart {
			body = append(body, theme.RuleText().Render(strings.Repeat("─", minInt(w, 80))))
		}
		prevRun = e.Run
		body = append(body, traceEventLines(e, w)...)
	}
	room := maxInt(h-len(head), 1)
	tp.scroll = clampInt(tp.scroll, 0, maxInt(len(body)-room, 0))
	body = body[tp.scroll:]
	if len(body) > room {
		body = body[:room]
	}
	return strings.Join(append(head, body...), "\n")
}

// traceRunStrip lists the runs as chips: all, then each run with its
// attempt and outcome; the selected one lit.
func (m *Model) traceRunStrip(tp *tracePane, w int) string {
	chip := func(label string, on bool, fg lipgloss.Style) string {
		if on {
			return theme.Chip().Render(" " + label + " ")
		}
		return fg.Render(" " + label + " ")
	}
	parts := []string{chip(fmt.Sprintf("all runs (%d)", len(tp.tr.Runs)), tp.run == -1, theme.TextDim())}
	for i, r := range tp.tr.Runs {
		st := theme.TextDim()
		switch r.Status {
		case tasks.RunOK:
			st = theme.SuccessText()
		case tasks.RunError, tasks.RunTimeout:
			st = theme.DangerText()
		}
		label := fmt.Sprintf("%d %s %s", i+1, r.Agent, r.Started.Format("15:04"))
		if r.Status != "" && r.Status != tasks.RunOK {
			label += " " + string(r.Status)
		}
		parts = append(parts, chip(label, tp.run == i, st))
	}
	return kit.ClipEllipsis(strings.Join(parts, " "), w)
}

// traceEventLines renders one event: time, actor, a kind tag, then the
// text wrapped under a hanging indent (prompt texts collapse to their
// first lines; export keeps them whole).
func traceEventLines(e trace.Event, w int) []string {
	stamp := theme.TextMuted().Render(e.At.Format("15:04:05"))
	actor := theme.TextDim().Render(padTo(kit.ClipEllipsis(orDash(e.Actor), 10), 10))
	if e.Actor != "" && e.Actor != bus.Human {
		actor = lipgloss.NewStyle().Foreground(theme.AuthorColor(e.Actor)).Render(padTo(kit.ClipEllipsis(e.Actor, 10), 10))
	}
	tag, body := traceTag(e.Kind)
	text := strings.TrimSpace(e.Text)
	if e.Kind == "prompt_text" || e.Kind == "system_text" {
		ls := strings.Split(text, "\n")
		if len(ls) > 6 {
			text = strings.Join(ls[:6], "\n") + fmt.Sprintf("\n… %d more lines (x exports the full text)", len(ls)-6)
		}
	}
	lead := stamp + " " + actor + " " + tag + " "
	indent := strings.Repeat(" ", 8+1+10+1+ansi.Width(tag)+1)
	textW := maxInt(w-len([]rune(indent)), 12)
	var out []string
	first := true
	for _, para := range strings.Split(text, "\n") {
		for _, l := range kit.WrapWords(para, textW) {
			if first {
				out = append(out, lead+body.Render(l))
				first = false
				continue
			}
			out = append(out, indent+body.Render(l))
		}
	}
	if first {
		out = append(out, lead)
	}
	return out
}

// traceTag is the kind marker and the body style for an event kind.
func traceTag(kind string) (string, lipgloss.Style) {
	switch kind {
	case "command":
		return theme.AccentText().Render("❯"), theme.TextStyle()
	case "error", "stderr":
		return theme.DangerText().Render("✗"), theme.DangerText()
	case "final":
		return theme.TextMuted().Render("▸"), theme.TextMuted()
	case "prompt", "prompt_text", "system_text":
		return theme.InfoText().Render("→"), theme.TextDim()
	case trace.KindRunStart:
		return theme.AccentText().Render("▶"), theme.TextStyle().Bold(true)
	case trace.KindRunEnd:
		return theme.AccentText().Render("■"), theme.TextDim()
	case trace.KindReply, trace.KindMessage:
		return theme.SuccessText().Render("“"), theme.TextStyle()
	case trace.KindAlert:
		return theme.WarningText().Render("!"), theme.WarningText()
	case trace.KindNotice:
		return theme.TextMuted().Render("·"), theme.TextDim()
	case trace.KindComment:
		return theme.InfoText().Render("≡"), theme.TextStyle()
	case trace.KindActivity:
		return theme.TextMuted().Render("~"), theme.TextMuted()
	}
	return theme.TextMuted().Render("·"), theme.TextDim()
}

// taskByslug looks a card up ("" store = none).
func (m *Model) taskByslug(slug string) (tasks.Task, bool) {
	if m.taskStore == nil {
		return tasks.Task{}, false
	}
	return m.taskStore.Get(slug)
}
