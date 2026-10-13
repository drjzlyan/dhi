package runtime

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// promptLimits bounds each volatile prompt block (F-065). Level 0 is the
// normal turn; level 1 is the compacted retry after a context overflow.
type promptLimits struct {
	history, msgRunes            int // conversation messages kept, runes per message
	journal, journalRunes, notes int // journal entries, runes each, notes runes
	kbHits, kbRunes              int // KB hits, runes per snippet
	comments, runs               int // task card comments and runs shown
	lessons                      int // learned lessons retrieved (F-068), beyond pinned
}

var promptLevels = [...]promptLimits{
	{history: historyWindow, msgRunes: 500, journal: 4, journalRunes: 200, notes: 1500, kbHits: 3, kbRunes: 300, comments: 5, runs: 3, lessons: 5},
	{history: 12, msgRunes: 240, journal: 2, journalRunes: 160, notes: 600, kbHits: 1, kbRunes: 200, comments: 2, runs: 1, lessons: 2},
	// F-078: a third, tightest level, so an overflow after the first
	// compaction still has somewhere to go.
	{history: 6, msgRunes: 160, journal: 1, journalRunes: 120, notes: 300, kbHits: 0, kbRunes: 0, comments: 1, runs: 1, lessons: 1},
}

// maxCompaction is the most compact level; an overflow there is final.
const maxCompaction = len(promptLevels) - 1

func limitsAt(level int) promptLimits {
	return promptLevels[min(max(level, 0), maxCompaction)]
}

// conversationWindow is how many of a channel's newest records a prompt
// considers before filtering to the trigger's thread.
const conversationWindow = 4000

// conversation is what agentID's prompt replays for trigger (F-065): a
// threaded trigger sees its own thread (root first, then the replies), a
// top-level trigger the channel's top level. Relayed stream lines are
// status, not conversation; of the agent's own messages only its marked
// final replies replay (legacy unmarked lines may be progress noise).
func (r *Runtime) conversation(agentID string, trigger bus.Message) []bus.Message {
	// A bounded window of the channel's newest records (F-078): a channel
	// grows for months; the prompt replays recent conversation, and the
	// episode's summary carries what came before.
	msgs := r.cfg.Bus.Recent(trigger.Channel, trigger.Thread, conversationWindow)
	out := msgs[:0]
	for _, m := range msgs {
		switch {
		case m.ID == trigger.ID, m.Kind == bus.KindStream:
		case m.Author == agentID && m.Kind != bus.KindReply:
		default:
			out = append(out, m)
		}
	}
	return out
}

// historyBlock renders the conversation for agentID within lim, naming
// how much was left out. after > 0 keeps only messages newer than it (a
// resumed session already holds the rest).
func (r *Runtime) historyBlock(agentID string, trigger bus.Message, lim promptLimits, after int64) (head, body string) {
	msgs := r.conversation(agentID, trigger)
	if after > 0 {
		kept := msgs[:0]
		for _, m := range msgs {
			if m.ID > after {
				kept = append(kept, m)
			}
		}
		msgs = kept
	}
	if len(msgs) == 0 {
		return "", ""
	}
	var h, b strings.Builder
	h.WriteString("\n\nEarlier in this thread:")
	if n := len(msgs) - lim.history; n > 0 {
		fmt.Fprintf(&h, "\n- (%d earlier messages omitted)", n)
		if brief := briefOf(msgs[:n], agentID); brief != "" {
			h.WriteString(" In brief:" + brief)
		}
		msgs = msgs[n:]
	}
	for _, m := range msgs {
		prefix := m.Author
		switch m.Author {
		case bus.Human:
			prefix = "you (the human)"
		case agentID:
			prefix = "you (earlier)"
		}
		b.WriteString("\n- " + prefix + ": " + truncateRunes(m.Text, lim.msgRunes))
	}
	return h.String(), b.String()
}

// taskBlock renders the card bound to the trigger's thread, if any, so
// the agent works from the task's state rather than rediscovering it.
func (r *Runtime) taskBlock(agentID string, trigger bus.Message, lim promptLimits) string {
	t, ok := r.boundTask(trigger)
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nTask card %s: %s\n- status: %s", t.Slug, t.Title, t.Status)
	if t.Priority != "" {
		fmt.Fprintf(&b, " · priority %s", t.Priority)
	}
	if len(t.Labels) > 0 {
		b.WriteString(" · labels " + strings.Join(t.Labels, ", "))
	}
	if t.Workflow != "" {
		fmt.Fprintf(&b, "\n- workflow: %s · tests passing: %v", t.Workflow, t.TestsPass)
	}
	if len(t.ChangeSets) > 0 {
		cs := t.ChangeSets[0]
		fmt.Fprintf(&b, "\n- worktree: %s on branch %s — DHI created it and it is your working directory; work and commit only there."+
			" It is member %q's checkout, so a path written %s/<file> means <file> inside this worktree",
			cs.Path, cs.Branch, cs.Member, cs.Member)
	}
	if vt := r.verifyTargets(t, r.cliWorkdir(trigger)); len(vt) > 0 && r.cfg.Run != nil {
		fmt.Fprintf(&b, "\n- when you finish, DHI verifies with `%s`; a failure comes back to you", vt[0])
	}
	if t.PRURL != "" {
		b.WriteString("\n- pull request: " + t.PRURL)
	}
	b.WriteString(r.teamBlock(agentID, t))
	if c := lastN(t.Comments, lim.comments); len(c) > 0 {
		b.WriteString("\n- recent comments:")
		for _, cm := range c {
			b.WriteString("\n  - " + cm.Author + ": " + truncateRunes(cm.Text, 200))
		}
	}
	if rs := lastN(t.Runs, lim.runs); len(rs) > 0 {
		b.WriteString("\n- earlier runs:")
		for _, run := range rs {
			detail := run.Summary
			if run.Status != tasks.RunOK {
				detail = run.Error
			}
			fmt.Fprintf(&b, "\n  - %s by %s: %s", run.Status, run.Agent, truncateRunes(detail, 200))
		}
	}
	return b.String()
}

func lastN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// failureBrief tells the next attempt why the last one failed and where
// it stopped (F-065), so a retry continues instead of starting blind.
func failureBrief(f clirun.Failure, run tasks.Run, events []clirun.StreamEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nYour previous attempt (%d) failed [%s]: %s", run.Attempt+1, f, truncateRunes(run.Error, 300))
	var tail []string
	for _, ev := range events {
		if ev.Kind != clirun.EventFinal && strings.TrimSpace(ev.Detail) != "" {
			tail = append(tail, truncateRunes(ev.Detail, 200))
		}
	}
	if tail = lastN(tail, 8); len(tail) > 0 {
		b.WriteString("\nIts last steps were:\n- " + strings.Join(tail, "\n- "))
	}
	b.WriteString("\nCheck what already exists before acting, continue from there, and do not redo finished work.")
	return b.String()
}

// retryWait is the pause before the next attempt: none after an overflow
// (the compacted prompt is the fix), exponential with jitter after a
// rate limit, the base backoff otherwise.
func retryWait(f clirun.Failure, attempt int) time.Duration {
	switch f {
	case clirun.FailOverflow:
		return 0
	case clirun.FailRateLimit:
		d := cliRetryBackoff << min(attempt+1, 5)
		return d + time.Duration(rand.Int64N(int64(d/4)+1))
	}
	return cliRetryBackoff
}

// authHint is the named notice for a failure retrying cannot fix.
func authHint(c *clirun.CLI) string {
	return fmt.Sprintf("%s is not authenticated — run `%s` in a terminal and log in, then ask again", c.Name, c.Bin)
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

// lastLine is the last non-blank line written, trimmed.
func (t *tailBuffer) lastLine() string {
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// briefOf is an extractive summary of messages dropped from the history
// window (F-078): who said what, one short line each, the newest lines
// kept — so a long conversation is condensed, not forgotten. No model
// call: it costs nothing and cannot invent anything.
func briefOf(msgs []bus.Message, agentID string) string {
	const lines, perLine, total = 12, 110, 1200
	var out []string
	for _, m := range msgs {
		who := m.Author
		switch m.Author {
		case bus.Human:
			who = "human"
		case agentID:
			who = "you"
		}
		first := strings.Join(strings.Fields(m.Text), " ")
		if i := strings.IndexAny(first, ".!?"); i > 20 {
			first = first[:i+1]
		}
		out = append(out, "\n  · "+who+": "+truncateRunes(first, perLine))
	}
	if len(out) > lines {
		out = out[len(out)-lines:]
	}
	s := strings.Join(out, "")
	if r := []rune(s); len(r) > total {
		s = string(r[len(r)-total:])
	}
	return s
}
