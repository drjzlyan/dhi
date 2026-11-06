package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/untrusted"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-182: the AI parts ({ai:…} slots) of a team's branch, commit and PR
// templates. When an agent commits or opens a PR it writes them itself
// (git_commit / pr_open `fill`). When nobody is in the loop — a human's
// commit or PR, a branch named before a task turn — DHI asks for them in
// one short, read-only call, the way :suggest does (F-181): no tools, no
// session resumed, nothing posted, metered as its own ledger kind. What
// it reads (a diff, commits, a task) is data, inside the untrusted fence.

const (
	writeContextRunes = 12000 // the most context sent
	writeSlotRunes    = 2000  // the longest text kept for one slot
	writeTimeout      = 90 * time.Second
)

// writePrompt asks for one JSON object with a string per slot key.
func writePrompt(req conventions.GenRequest) string {
	var b strings.Builder
	what := req.What
	if what == "" {
		what = "text"
	}
	fmt.Fprintf(&b, "You write parts of a %s for a software team. The team's format is below; "+
		"DHI fills everything except the parts in <…>, which are yours:\n\n%s\n\n", what, req.Shape)
	b.WriteString("Reply with ONLY one JSON object, no code fence, mapping each key to the text for that part:\n")
	b.WriteString(conventions.SlotList(req.Slots) + "\n")
	b.WriteString("Write plain text in each value: no markdown fence, and do not repeat the fixed parts. " +
		"A part that sits on one line in the format must be one line. Base it on the context only. " +
		"Do not use tools. " + untrusted.Rule + "\n\n")
	b.WriteString(untrusted.Wrap("context", shrinkRunes(req.Context, writeContextRunes, false)))
	return b.String()
}

// WriteSlots runs the one-shot read-only call on agentID and returns the
// text for every requested slot. It refuses by name for a CLI without a
// read-only mode (ADR-0011) and when a budget is reached, starting
// nothing; a reply missing a slot is an error naming it.
func (r *Runtime) WriteSlots(ctx context.Context, agentID string, req conventions.GenRequest) (map[string]string, error) {
	if len(req.Slots) == 0 {
		return map[string]string{}, nil
	}
	r.mu.Lock()
	e, ok := r.agents[agentID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("runtime: unknown agent %q", agentID)
	}
	if !e.cli.ReadOnlyOK {
		return nil, fmt.Errorf("%s (%s) has no read-only mode (%s), so it cannot write the AI parts here; nothing was started — write them yourself, or make an agent on claude or codex the default", agentID, e.cli.Name, e.cli.ReadOnlyWhy)
	}
	tune := r.tuning()
	if msg, ok := r.budgetCheck(agentID, "", tune.Budgets); !ok {
		return nil, fmt.Errorf("budget reached: %s", msg)
	}
	limit := writeTimeout
	if e.m.Timeout > 0 && e.m.Timeout < limit {
		limit = e.m.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	trigger := bus.Message{Channel: "dm:" + agentID}
	run, _ := r.cliSpawnOnce(ctx, e, trigger, spawnInput{
		prompt: writePrompt(req), workdir: r.cfg.WS.Root, maxTurns: 1,
		stall: tune.Stall, quiet: true, readOnly: true, budgets: tune.Budgets,
	})
	r.recordSpendKind(run, "", budget.KindWrite)
	if run.Status != tasks.RunOK {
		return nil, fmt.Errorf("%s could not write it: %s", agentID, run.Error)
	}
	return parseSlots(run.Summary, req.Slots)
}

// parseSlots reads the reply's JSON object (the first {…} in it, so a
// sentence around it is tolerated) and keeps the requested keys.
func parseSlots(reply string, slots []conventions.Slot) (map[string]string, error) {
	i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if i < 0 || j <= i {
		return nil, fmt.Errorf("the reply was not a JSON object: %q", shrinkRunes(reply, 120, false))
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(reply[i:j+1]), &raw); err != nil {
		return nil, fmt.Errorf("the reply was not a JSON object: %v", err)
	}
	got := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			got[k] = s
		}
	}
	got = conventions.NormalizeFill(got)
	out := map[string]string{}
	var missing []string
	for _, sl := range slots {
		v := strings.TrimSpace(got[sl.Key])
		if v == "" {
			missing = append(missing, fmt.Sprintf("%q", sl.Key))
			continue
		}
		if rs := []rune(v); len(rs) > writeSlotRunes {
			v = string(rs[:writeSlotRunes])
		}
		out[sl.Key] = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the reply had no text for %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// Writer is WriteSlots on the default agent, as a conventions.Generator
// for the surfaces and services that open PRs, commit, or name branches
// for a human. It names the agent it used in any error.
func (r *Runtime) Writer() conventions.Generator {
	return func(ctx context.Context, req conventions.GenRequest) (map[string]string, error) {
		id := r.DefaultAgent()
		if id == "" {
			return nil, fmt.Errorf("no agent to write the AI parts: add one to the team, or write them yourself")
		}
		return r.WriteSlots(ctx, id, req)
	}
}
