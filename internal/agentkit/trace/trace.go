// Package trace assembles a task's debugging timeline (F-074): every run
// and attempt with its transcript events, the bound thread's messages
// (tool lines and runtime notices included), comments and activity —
// merged in time order — and exports it as Markdown or JSONL. It is pure:
// callers hand it the card, the thread and a transcript reader.
package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// Event kinds beyond the transcript's own (progress, command, error,
// final, prompt, prompt_text, system_text, stderr).
const (
	KindRunStart = "run"
	KindRunEnd   = "run_end"
	KindMessage  = "message"
	KindReply    = "reply"
	KindAlert    = "alert"
	KindNotice   = "notice"
	KindComment  = "comment"
	KindActivity = "activity"
)

// Event is one timeline entry.
type Event struct {
	At      time.Time `json:"at"`
	Run     string    `json:"run,omitempty"` // run id for run-scoped events
	Attempt int       `json:"attempt,omitempty"`
	Actor   string    `json:"actor,omitempty"`
	Kind    string    `json:"kind"`
	Text    string    `json:"text"`
}

// Record is one transcript line as the runtime persists it; older
// transcripts carry only kind and detail.
type Record struct {
	Kind    string    `json:"kind"`
	Detail  string    `json:"detail"`
	At      time.Time `json:"at,omitzero"`
	Attempt int       `json:"attempt,omitempty"`
}

// Input is everything a trace is built from.
type Input struct {
	Task tasks.Task
	// Thread is the bound thread: its root and replies, every kind.
	Thread []bus.Message
	// Read loads a run's transcript (nil or an error = no events; the
	// run still shows with its outcome).
	Read func(path string) ([]Record, error)
}

// Trace is a built timeline.
type Trace struct {
	Task   tasks.Task
	Runs   []tasks.Run
	Events []Event
	// Missing names runs whose transcript could not be read.
	Missing []string
}

// Build merges the sources into one time-ordered timeline. Relayed
// stream lines that duplicate a run's own transcript are dropped; the
// runtime's notices (retries, verify results) stay.
func Build(in Input) Trace {
	t := Trace{Task: in.Task, Runs: append([]tasks.Run(nil), in.Task.Runs...)}
	seen := map[string]bool{} // transcript details, to drop relayed copies
	var evs []Event
	for _, r := range t.Runs {
		evs = append(evs, Event{At: r.Started, Run: r.ID, Attempt: r.Attempt, Actor: r.Agent, Kind: KindRunStart,
			Text: runHead(r)})
		var recs []Record
		var err error
		if in.Read != nil && r.Transcript != "" {
			recs, err = in.Read(r.Transcript)
		}
		if err != nil {
			t.Missing = append(t.Missing, r.ID)
		}
		for i, rec := range recs {
			at := rec.At
			if at.IsZero() { // legacy record: keep file order inside the run
				at = r.Started.Add(time.Duration(i+1) * time.Microsecond)
			}
			seen[strings.TrimSpace(rec.Detail)] = true
			evs = append(evs, Event{At: at, Run: r.ID, Attempt: r.Attempt, Actor: r.Agent, Kind: rec.Kind, Text: rec.Detail})
		}
		end := r.Finished
		if end.IsZero() {
			end = r.Started
		}
		evs = append(evs, Event{At: end.Add(time.Nanosecond), Run: r.ID, Attempt: r.Attempt, Actor: r.Agent,
			Kind: KindRunEnd, Text: runTail(r)})
	}
	for _, m := range in.Thread {
		kind := KindMessage
		switch m.Kind {
		case bus.KindStream:
			if seen[strings.TrimSpace(m.Text)] {
				continue // the run's transcript already has it
			}
			kind = KindNotice
		case bus.KindReply:
			kind = KindReply
		case bus.KindAlert:
			kind = KindAlert
		}
		evs = append(evs, Event{At: m.At, Actor: m.Author, Kind: kind, Text: m.Text})
	}
	for _, c := range in.Task.Comments {
		evs = append(evs, Event{At: c.At, Actor: c.Author, Kind: KindComment, Text: c.Text})
	}
	for _, a := range in.Task.Activity {
		evs = append(evs, Event{At: a.At, Actor: a.Actor, Kind: KindActivity, Text: activityText(a)})
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	t.Events = evs
	return t
}

func runHead(r tasks.Run) string {
	s := fmt.Sprintf("%s started (attempt %d) · %s", r.ID, r.Attempt+1, orDash(r.Runtime))
	if r.Model != "" {
		s += " · " + r.Model
	}
	return s
}

func runTail(r tasks.Run) string {
	s := fmt.Sprintf("%s %s", r.ID, r.Status)
	if !r.Finished.IsZero() && !r.Started.IsZero() {
		s += " in " + r.Finished.Sub(r.Started).Round(time.Second).String()
	}
	if r.TokensIn >= 0 && r.TokensOut >= 0 {
		s += fmt.Sprintf(" · %d in / %d out tokens", r.TokensIn, r.TokensOut)
	}
	if r.CacheRead > 0 || r.CacheWrite > 0 {
		s += fmt.Sprintf(" · cache %d read / %d write", r.CacheRead, r.CacheWrite)
	}
	if r.HasCost {
		s += fmt.Sprintf(" · $%.4f", r.CostUSD)
	}
	if r.Error != "" {
		s += " · " + r.Error
	}
	return s
}

func activityText(a tasks.Activity) string {
	switch a.Kind {
	case tasks.ActStatus:
		return "moved " + a.From + " → " + a.To
	case tasks.ActAssignee:
		return "assignee " + orDash(a.From) + " → " + orDash(a.To)
	}
	if a.From != "" {
		return a.Kind + " " + a.From + " → " + a.To
	}
	return strings.TrimSpace(a.Kind + " " + a.To)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Filter narrows events: run "" keeps every run (and the non-run
// events); kinds nil keeps every kind; query matches text or actor.
func (t Trace) Filter(run string, kinds map[string]bool, query string) []Event {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Event
	for _, e := range t.Events {
		if run != "" && e.Run != run {
			continue
		}
		if kinds != nil && !kinds[e.Kind] {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Text+" "+e.Actor), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// WriteJSONL writes one JSON object per event.
func (t Trace) WriteJSONL(w io.Writer, evs []Event) error {
	enc := json.NewEncoder(w)
	for _, e := range evs {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// WriteMarkdown writes a readable report: the card, a run table, then
// the timeline with multi-line text in fenced blocks.
func (t Trace) WriteMarkdown(w io.Writer, evs []Event) error {
	var b strings.Builder
	tk := t.Task
	fmt.Fprintf(&b, "# Trace: %s\n\n%s\n\n", tk.Slug, tk.Title)
	fmt.Fprintf(&b, "- status: %s\n- assignee: %s\n", tk.Status, orDash(tk.Assignee))
	for _, cs := range tk.ChangeSets {
		fmt.Fprintf(&b, "- worktree: %s (%s, branch %s)\n", cs.Path, cs.Member, cs.Branch)
	}
	if len(t.Runs) > 0 {
		b.WriteString("\n## Runs\n\n| run | agent | attempt | status | tokens in/out | cost |\n|---|---|---|---|---|---|\n")
		for _, r := range t.Runs {
			cost := "-"
			if r.HasCost {
				cost = fmt.Sprintf("$%.4f", r.CostUSD)
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %s | %d/%d | %s |\n", r.ID, r.Agent, r.Attempt+1, r.Status, r.TokensIn, r.TokensOut, cost)
		}
	}
	b.WriteString("\n## Timeline\n\n")
	for _, e := range evs {
		head := fmt.Sprintf("- `%s` **%s** %s", e.At.Format("2006-01-02 15:04:05"), orDash(e.Actor), e.Kind)
		if e.Run != "" {
			head += " (" + e.Run + ")"
		}
		text := strings.TrimRight(e.Text, "\n")
		if strings.Contains(text, "\n") || len(text) > 160 {
			fmt.Fprintf(&b, "%s\n\n  ```\n  %s\n  ```\n", head, strings.ReplaceAll(strings.ReplaceAll(text, "```", "'''"), "\n", "\n  "))
		} else {
			fmt.Fprintf(&b, "%s: %s\n", head, text)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
