package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/untrusted"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-181 (ADR-0051): :suggest in the editor. One short, read-only spawn on
// the user's default agent, asked for a continuation at the cursor. It is
// never a chat turn: nothing is posted, no episode is opened, the CLI's
// session is not resumed, and the call is metered as its own ledger line.
// The file excerpt is data, so it sits inside the untrusted fence
// (ADR-0046) with the cursor marked.

const (
	suggestBeforeRunes = 3000 // newest text before the cursor that is sent
	suggestAfterRunes  = 1000 // text after the cursor that is sent
	suggestReplyRunes  = 2000 // the longest continuation kept
	suggestTimeout     = 90 * time.Second

	// SuggestCursor marks the cursor inside the excerpt.
	SuggestCursor = "<<CURSOR>>"
)

// SuggestRequest is the text around the cursor: Before is everything up
// to it, After everything following. The runtime bounds both.
type SuggestRequest struct {
	Path, Language string
	Before, After  string
}

// suggestPrompt is the bounded prompt: the instruction, then the excerpt
// fenced as data.
func suggestPrompt(q SuggestRequest) string {
	before := shrinkRunes(q.Before, suggestBeforeRunes, true)
	after := shrinkRunes(q.After, suggestAfterRunes, false)
	var b strings.Builder
	b.WriteString("You complete code at a cursor. Below is an excerpt of ")
	if q.Path != "" {
		b.WriteString("the file " + q.Path)
	} else {
		b.WriteString("a file")
	}
	if q.Language != "" {
		b.WriteString(" (" + q.Language + ")")
	}
	b.WriteString(" with the cursor marked " + SuggestCursor + ". Reply with ONLY the short text to insert at the cursor: " +
		"a few lines at most, no explanation, no code fence, and do not repeat the text before the cursor. " +
		"Do not use tools. If nothing sensible follows, reply with nothing. " + untrusted.Rule + "\n\n")
	b.WriteString(untrusted.Wrap("file excerpt", before+SuggestCursor+after))
	return b.String()
}

// Suggest runs the one-shot read-only call on agentID and returns the
// continuation text ("" when the model had nothing to add). It refuses by
// name for a CLI without a read-only mode (ADR-0011) and when a budget is
// reached, and it starts nothing in either case.
func (r *Runtime) Suggest(ctx context.Context, agentID string, q SuggestRequest) (string, error) {
	r.mu.Lock()
	e, ok := r.agents[agentID]
	r.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("runtime: unknown agent %q", agentID)
	}
	if !e.cli.ReadOnlyOK {
		return "", fmt.Errorf("%s has no read-only mode (%s); nothing was started", e.cli.Name, e.cli.ReadOnlyWhy)
	}
	tune := r.tuning()
	if msg, ok := r.budgetCheck(agentID, "", tune.Budgets); !ok {
		return "", fmt.Errorf("budget reached: %s", msg)
	}
	limit := suggestTimeout
	if e.m.Timeout > 0 && e.m.Timeout < limit {
		limit = e.m.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	trigger := bus.Message{Channel: "dm:" + agentID}
	run, _ := r.cliSpawnOnce(ctx, e, trigger, spawnInput{
		prompt: suggestPrompt(q), workdir: r.cfg.WS.Root, maxTurns: 1,
		stall: tune.Stall, quiet: true, readOnly: true, budgets: tune.Budgets,
	})
	r.recordSpendKind(run, "", budget.KindSuggest)
	if run.Status != tasks.RunOK {
		return "", fmt.Errorf("the suggestion failed: %s", run.Error)
	}
	return cleanSuggestion(run.Summary), nil
}

// cleanSuggestion trims the model's reply to the text to insert: no
// surrounding fence, no leading blank lines, bounded.
func cleanSuggestion(s string) string {
	s = strings.ReplaceAll(strings.TrimRight(s, " \t\r\n"), "\r\n", "\n")
	if lines := strings.Split(s, "\n"); len(lines) >= 2 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") &&
		strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		s = strings.Join(lines[1:len(lines)-1], "\n")
	}
	s = strings.TrimLeft(s, "\n")
	if rs := []rune(s); len(rs) > suggestReplyRunes {
		s = string(rs[:suggestReplyRunes]) // a cut, not an ellipsis: this is inserted as code
	}
	return s
}
