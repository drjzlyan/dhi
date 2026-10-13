package runtime

import (
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// Tuning is the live agent-driving configuration (F-066), read per turn
// so a Settings change governs the next spawn without a restart.
type Tuning struct {
	MaxTurns int           // per-spawn CLI turn cap; 0 = adapter default
	Resume   bool          // continue verified CLI sessions across turns
	Budgets  budget.Limits // spend caps; zero = none

	// F-067 goal loop.
	Autonomy      string        // "envelope" | "ask" (settings.Agents.Autonomy)
	MaxIterations int           // verify → fix rounds per task turn (min 1)
	Stall         time.Duration // watchdog; 0 = off

	// F-068 learning.
	Reflect      bool   // reflect on turns that carried a signal
	ReflectModel string // "" = the agent's model

	// F-074: save full prompts into run transcripts.
	TracePrompts bool
}

// tuning reads the configured Tuning; without a source, resume is on
// and nothing is capped (the settings defaults).
func (r *Runtime) tuning() Tuning {
	if r.cfg.Tuning == nil {
		return Tuning{Resume: true, Autonomy: "envelope", MaxIterations: 3, Reflect: true}
	}
	return r.cfg.Tuning()
}

// openEpisode loads (or starts) the agent's episode for this turn. A
// store failure degrades to an in-memory episode: the turn still runs,
// it just starts fresh (resume is an optimisation, never a gate).
func (r *Runtime) openEpisode(agentID, taskSlug string, trigger bus.Message) episode.Episode {
	key := episode.Key(taskSlug, trigger.Channel, bus.ThreadOf(trigger))
	fresh := episode.Episode{
		Key: key, Agent: agentID, Started: time.Now().UTC(),
		Goal: truncateRunes(strings.TrimSpace(stripMention(trigger.Text, agentID)), 300),
	}
	if r.cfg.Episodes == nil {
		return fresh
	}
	ep, ok, err := r.cfg.Episodes.Get(agentID, key)
	if err != nil {
		// Named, never silent (F-078): the turn starts fresh and the
		// trace says why its earlier session was not continued.
		r.notice(trigger, agentID, "could not read this conversation's episode, starting fresh: "+err.Error())
		return fresh
	}
	if !ok {
		return fresh
	}
	return ep
}

// saveEpisode writes the turn's episode, keeping the plan the agent
// published mid-turn through plan_update (the tool owns that field).
func (r *Runtime) saveEpisode(ep episode.Episode) {
	if r.cfg.Episodes == nil {
		return
	}
	_ = r.cfg.Episodes.Update(ep.Agent, ep.Key, func(cur *episode.Episode) {
		plan := cur.Plan
		*cur = ep
		cur.Plan = plan
	})
}

// budgetCheck tallies the spend ledger against the limits before a
// spawn. No ledger or no limits = always allowed. A ledger read failure
// refuses by name — a budget that cannot be checked is not enforced
// silently (ADR-0011).
func (r *Runtime) budgetCheck(agentID, taskSlug string, lim budget.Limits) (string, bool) {
	if r.cfg.Spend == nil || lim.None() {
		return "", true
	}
	entries, err := r.cfg.Spend.Entries()
	if err != nil {
		return "the spend ledger is unreadable (" + err.Error() + ")", false
	}
	return budget.Check(lim, budget.Tally(entries, agentID, taskSlug, budget.DayStart(time.Now())), taskSlug != "")
}

// recordSpend appends one run to the spend ledger (best-effort: the run
// record on the card is the durable copy).
func (r *Runtime) recordSpend(run tasks.Run, taskSlug string) {
	if r.cfg.Spend == nil {
		return
	}
	tokens := run.CacheRead + run.CacheWrite
	if run.TokensIn > 0 {
		tokens += run.TokensIn
	}
	if run.TokensOut > 0 {
		tokens += run.TokensOut
	}
	_ = r.cfg.Spend.Append(budget.Entry{
		At: run.Finished, Agent: run.Agent, Task: taskSlug,
		Tokens: tokens, CostUSD: run.CostUSD,
	})
}
