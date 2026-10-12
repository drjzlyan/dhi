package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// turnLog collects what a turn learned the hard way (F-068): the signals
// that make reflection worth its tokens, and the evidence it reads.
type turnLog struct {
	signals    []lessons.Signal
	failures   []string
	runs       []string
	outcome    string
	correction string
	previous   string // the agent's reply the correction answers
}

func (tl *turnLog) signal(s lessons.Signal) {
	for _, x := range tl.signals {
		if x == s {
			return
		}
	}
	tl.signals = append(tl.signals, s)
}

func (tl *turnLog) fail(text string) {
	if text = strings.TrimSpace(text); text != "" && len(tl.failures) < 6 {
		tl.failures = append(tl.failures, truncateRunes(text, 900))
	}
}

// noteCorrection marks a human trigger that corrects the agent's earlier
// reply in the same conversation — the cheapest strong learning signal.
func (r *Runtime) noteCorrection(agentID string, trigger bus.Message, tl *turnLog) {
	if trigger.Author != bus.Human || !lessons.IsCorrection(stripMention(trigger.Text, agentID)) {
		return
	}
	// The agent's latest reply before the trigger, at the top level or
	// in the trigger's thread (a reply to a top-level ask is posted top
	// level, while the human may answer it in a thread).
	msgs := r.cfg.Bus.History(trigger.Channel, 0)
	if trigger.Thread != 0 {
		msgs = append(msgs, r.cfg.Bus.History(trigger.Channel, trigger.Thread)...)
	}
	var prev string
	var prevID int64
	for _, m := range msgs {
		if m.Author == agentID && m.Kind == bus.KindReply && m.ID < trigger.ID && m.ID > prevID {
			prev, prevID = m.Text, m.ID
		}
	}
	if prev == "" {
		return // nothing of the agent's to correct
	}
	tl.signal(lessons.SignalCorrection)
	tl.correction = strings.TrimSpace(stripMention(trigger.Text, agentID))
	tl.previous = prev
}

// reflect runs the post-turn self-review when the turn carried a signal:
// one short, tool-less spawn whose lessons are stored immediately (auto,
// reviewable in the Brain) and named in the thread.
func (r *Runtime) reflect(ctx context.Context, e *entry, trigger bus.Message, tl *turnLog) {
	tune := r.tuning()
	if len(tl.signals) == 0 || !tune.Reflect || r.cfg.Lessons == nil {
		return
	}
	task, _ := r.boundTask(trigger)
	if msg, ok := r.budgetCheck(e.m.ID, task.Slug, tune.Budgets); !ok {
		r.notice(trigger, e.m.ID, "skipped learning from this turn: "+msg)
		return
	}
	var known []string
	if have, err := r.cfg.Lessons.List(e.m.ID); err == nil {
		for _, l := range have {
			if l.Status != lessons.StatusRetired && len(known) < 12 {
				known = append(known, l.Text)
			}
		}
	}
	outcome := tl.outcome
	if tl.previous != "" {
		outcome = "your earlier reply was: " + truncateRunes(tl.previous, 400)
	}
	prompt := lessons.ReflectionPrompt(lessons.Evidence{
		Agent: e.m.ID, Goal: stripMention(trigger.Text, e.m.ID), Outcome: outcome,
		Signals: tl.signals, Failures: tl.failures, Correction: tl.correction, Known: known,
	})
	model := e.m.CLIModel()
	if tune.ReflectModel != "" {
		model = tune.ReflectModel
	}
	run, _ := r.cliSpawnOnce(ctx, e, trigger, spawnInput{
		prompt: prompt, workdir: r.cliWorkdir(trigger), maxTurns: 1, stall: tune.Stall,
		model: model, quiet: true,
	})
	r.recordSpend(run, task.Slug)
	if run.Status != tasks.RunOK {
		r.notice(trigger, e.m.ID, "could not learn from this turn: "+run.Error)
		return
	}
	found, err := lessons.ParseReflection(run.Summary)
	if err != nil {
		r.notice(trigger, e.m.ID, "could not learn from this turn: "+err.Error())
		return
	}
	source := lessons.SourceReflection
	if tl.correction != "" {
		source = lessons.SourceCorrection
	}
	for _, l := range found {
		l.Agent, l.Source, l.Runs = e.m.ID, source, tl.runs
		stored, added, err := r.cfg.Lessons.Add(l)
		switch {
		case err != nil:
			r.notice(trigger, e.m.ID, "could not store a lesson: "+err.Error())
		case added:
			r.notice(trigger, e.m.ID, "learned: "+stored.Text)
		default:
			r.notice(trigger, e.m.ID, "reinforced: "+stored.Text)
		}
	}
}

// lessonsBlock injects what the agent has learned, ahead of the journal:
// every pinned lesson and the most relevant live ones (F-068).
func (r *Runtime) lessonsBlock(agentID, query string, lim promptLimits) string {
	if r.cfg.Lessons == nil || lim.lessons == 0 {
		return ""
	}
	ls, err := r.cfg.Lessons.Retrieve(agentID, query, lim.lessons)
	if err != nil {
		return "\n\nLessons unavailable: " + err.Error()
	}
	if len(ls) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nLessons you have learned from past work (apply them):")
	for _, l := range ls {
		b.WriteString("\n- " + truncateRunes(l.Text, 240))
		if l.Scope == lessons.ScopeUser {
			b.WriteString(" (the human's preference)")
		}
	}
	return b.String()
}

// lessonRecorder is the lesson_record seam for one agent.
func (r *Runtime) lessonRecorder(agentID string) func(text, scope string, tags []string) (string, error) {
	if r.cfg.Lessons == nil {
		return nil
	}
	return func(text, scope string, tags []string) (string, error) {
		l, added, err := r.cfg.Lessons.Add(lessons.Lesson{
			Agent: agentID, Scope: scope, Text: text, Tags: tags,
			Confidence: 0.7, Source: lessons.SourceAgent,
		})
		if err != nil {
			return "", err
		}
		if added {
			return "remembered: " + l.Text, nil
		}
		return fmt.Sprintf("reinforced an existing lesson (confidence %.2f): %s", l.Confidence, l.Text), nil
	}
}

// consolidator runs each agent's local lesson consolidation at most once
// a day, at turn start (dedupe + decay; no tokens).
type consolidator struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (r *Runtime) consolidate(agentID string) {
	if r.cfg.Lessons == nil {
		return
	}
	r.cons.mu.Lock()
	if r.cons.last == nil {
		r.cons.last = map[string]time.Time{}
	}
	day := budget.DayStart(time.Now())
	if !r.cons.last[agentID].Before(day) {
		r.cons.mu.Unlock()
		return
	}
	r.cons.last[agentID] = day
	r.cons.mu.Unlock()
	_, _ = r.cfg.Lessons.Consolidate(agentID, 30)
}
