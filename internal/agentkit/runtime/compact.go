package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/untrusted"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-135: /compact. The user asks for a conversation's episode to be
// compacted now: a tool-less spawn on agents.reflect_model (else the
// conversation's model, else the agent's) writes a summary, the episode
// keeps it, and the kept CLI session is dropped. The next turn starts a
// fresh session from the summary and replays only the messages after it.

const (
	compactMsgRunes   = 1500  // per message in the transcript sent to the summarizer
	compactInputRunes = 60000 // newest part of the transcript that is sent
	compactKeepRunes  = 3000  // the stored summary
)

// Compact summarises the open conversation (channel, thread root) of
// agentID into its episode. It waits for a turn in flight, posts a notice
// when done or why not, and returns the same reason as an error.
func (r *Runtime) Compact(ctx context.Context, agentID, channel string, thread int64) error {
	r.mu.Lock()
	e, ok := r.agents[agentID]
	r.mu.Unlock()
	trigger := bus.Message{Channel: channel, Thread: thread}
	fail := func(format string, a ...any) error {
		err := fmt.Errorf(format, a...)
		r.notice(trigger, agentID, "could not compact: "+err.Error())
		return err
	}
	if !ok {
		return fail("%s is not on the crew", agentID)
	}
	if r.cfg.Episodes == nil {
		return fail("no episode store")
	}
	e.turnMu.Lock()
	defer e.turnMu.Unlock()
	r.workStart(agentKey(agentID))
	defer r.workDone(agentKey(agentID))

	task, _ := r.boundTask(trigger)
	key := episode.Key(task.Slug, channel, thread)
	ep, found, err := r.cfg.Episodes.Get(agentID, key)
	if err != nil {
		return fail("%v", err)
	}
	if !found {
		return fail("no conversation yet: %s has not replied in this thread", agentID)
	}
	var msgs []bus.Message
	for _, m := range r.cfg.Bus.Recent(channel, thread, conversationWindow) {
		if m.Kind != bus.KindStream && m.ID > ep.SummaryUpTo { // the summary already covers the rest
			msgs = append(msgs, m)
		}
	}
	if len(msgs) == 0 {
		return fail("nothing to compact")
	}
	upTo := msgs[len(msgs)-1].ID

	tune := r.tuning()
	if msg, ok := r.budgetCheck(agentID, task.Slug, tune.Budgets); !ok {
		return fail("%s", msg)
	}
	model := ep.Model
	if tune.ReflectModel != "" {
		model = tune.ReflectModel
	}
	run, _ := r.cliSpawnOnce(ctx, e, trigger, spawnInput{
		prompt: compactPrompt(ep, msgs, agentID), workdir: r.cliWorkdir(trigger), maxTurns: 1,
		stall: tune.Stall, model: model, quiet: true, budgets: tune.Budgets, taskSlug: task.Slug,
	})
	r.recordSpend(run, task.Slug)
	if run.Status != tasks.RunOK {
		return fail("%s", run.Error)
	}
	summary := truncateRunes(strings.TrimSpace(sanitizeReply(run.Summary)), compactKeepRunes)
	if summary == "" {
		return fail("the model returned no summary")
	}
	if err := r.cfg.Episodes.Compacted(agentID, key, summary, upTo); err != nil {
		return fail("%v", err)
	}
	r.notice(trigger, agentID, fmt.Sprintf("compacted: %d messages summarised; the next turn starts fresh from the summary", len(msgs)))
	return nil
}

// compactPrompt asks for the summary. The conversation is data, fenced.
func compactPrompt(ep episode.Episode, msgs []bus.Message, agentID string) string {
	var b strings.Builder
	for _, m := range msgs {
		who := m.Author
		switch m.Author {
		case bus.Human:
			who = "the human"
		case agentID:
			who = "you"
		}
		b.WriteString("- " + who + ": " + truncateRunes(m.Text, compactMsgRunes) + "\n")
	}
	var p strings.Builder
	p.WriteString("Compact this conversation so work can continue from your summary alone, " +
		"without the messages. Reply with the summary only, as plain text, under 300 words: " +
		"the goal, what was decided and done (files, branches, commands), the current state, " +
		"and the open questions and next steps. Keep names, paths and numbers exact. " +
		untrusted.Rule + "\n")
	if ep.Goal != "" {
		p.WriteString("\nThe opening request: " + ep.Goal + "\n")
	}
	if ep.Summary != "" {
		p.WriteString("\nYour earlier summary of what came before the messages below:\n" + untrusted.Wrap("episode summary", ep.Summary) + "\n")
	}
	p.WriteString("\nThe conversation:\n" + untrusted.Wrap("conversation", shrinkRunes(b.String(), compactInputRunes, true)))
	return p.String()
}
