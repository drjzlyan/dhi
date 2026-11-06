package runtime

import (
	"context"
	"fmt"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// Engine failover (F-180, ADR-0048). An agent may name a fallback engine
// (a visible setting, defaulted at setup). A run moves to it once, only for
// a closed list of failure classes (clirun.Failure.Failover) and only
// before the first engine ran any tool: after side effects the episode
// stays resumable and the human decides. Never silent: a thread notice, a
// toast, a transcript event and a run-record field name every use.

// failoverInfo labels the runs of a turn that ran on the fallback.
type failoverInfo struct {
	from string // the engine left, "cli:<name>"
	why  string // the failure class
}

// failoverEvent is the transcript record kind of a failover.
const failoverEvent = "engine_failover"

// fallbackEntry builds the entry a failover runs on: the same agent and
// guard on the fallback CLI, with the fallback's own model ("" = that
// CLI's default; the primary's model names belong to another vendor).
func (r *Runtime) fallbackEntry(e *entry) (*entry, error) {
	fb := e.m.FallbackRuntime
	if fb == "" {
		return nil, fmt.Errorf("no fallback engine is set")
	}
	if r.cfg.CLIs == nil {
		return nil, fmt.Errorf("no CLI registry")
	}
	c, ok := r.cfg.CLIs.Get(fb)
	if !ok {
		return nil, fmt.Errorf("unknown CLI %q", fb)
	}
	path, err := r.cfg.CLIs.Path(fb)
	if err != nil {
		return nil, fmt.Errorf("%s is not installed", c.Bin)
	}
	m := *e.m
	m.Runtime, m.Engine = fb, manifest.EngineString(fb)
	m.Model = manifest.ModelDefault
	if e.m.FallbackModel != "" {
		m.Model = e.m.FallbackModel
	}
	return &entry{m: &m, guard: e.guard, cli: c, cliPath: path}, nil
}

// ranTools reports whether a run's stream shows the CLI already ran a
// tool: past that point the work has side effects and no failover may
// replay it on another engine.
func ranTools(events []clirun.StreamEvent) bool {
	for _, ev := range events {
		if ev.Kind == clirun.EventCommand {
			return true
		}
	}
	return false
}

// failoverApproval asks before a fallback whose native-web or MCP wiring
// differs from the engine it replaces is used for the first time: the move
// changes what the agent can reach, which is never silent (F-123). The
// grant is remembered per agent and engine pair like any scope.
func (r *Runtime) failoverApproval(ctx context.Context, agentID string, from, to *clirun.CLI) error {
	if from == nil || to == nil {
		return nil
	}
	var diffs []string
	if from.WebDenyOK != to.WebDenyOK {
		diffs = append(diffs, fmt.Sprintf("its own web tools can be switched off on %s: %v, on %s: %v", from.Name, from.WebDenyOK, to.Name, to.WebDenyOK))
	}
	if from.MCPWired() != to.MCPWired() {
		diffs = append(diffs, fmt.Sprintf("DHI's IDE tools reach %s: %v, %s: %v", from.Name, from.MCPWired(), to.Name, to.MCPWired()))
	}
	if len(diffs) == 0 {
		return nil
	}
	if r.cfg.Approvals == nil {
		return fmt.Errorf("no approvals queue is running to ask about it")
	}
	reason := fmt.Sprintf("%s's fallback engine %s differs from %s: ", agentID, to.Name, from.Name)
	for i, d := range diffs {
		if i > 0 {
			reason += "; "
		}
		reason += d
	}
	return r.cfg.Approvals.AskScope(ctx, agentID, "failover:"+from.Name+">"+to.Name, sandbox.OpExec, to.Bin, reason)
}

// announceFailover tells everyone a run moved engines: the thread, the
// toast hook. The transcript event and run-record fields ride the next
// spawn (spawnInput.failover).
func (r *Runtime) announceFailover(trigger bus.Message, agentID, from, to string, why clirun.Failure) {
	r.notice(trigger, agentID, fmt.Sprintf("switching engine: %s → %s (%s). This is the fallback engine set for this agent; change it in Settings → AGENTS.", from, to, why))
	if r.cfg.OnFailover != nil {
		r.cfg.OnFailover(agentID, from, to, string(why))
	}
}

// tryFailover moves a failed turn to the agent's fallback when the rules
// allow it, returning the entry to continue on. A refusal is named in the
// thread and the turn fails as it would have without a fallback.
func (r *Runtime) tryFailover(ctx context.Context, trigger bus.Message, e *entry, fail clirun.Failure) (*entry, bool) {
	fe, err := r.fallbackEntry(e)
	if err != nil {
		r.notice(trigger, e.m.ID, fmt.Sprintf("not switching to the fallback engine cli:%s: %v", e.m.FallbackRuntime, err))
		return nil, false
	}
	if err := r.failoverApproval(ctx, e.m.ID, e.cli, fe.cli); err != nil {
		r.notice(trigger, e.m.ID, fmt.Sprintf("not switching to the fallback engine cli:%s: %v", fe.cli.Name, err))
		return nil, false
	}
	r.announceFailover(trigger, e.m.ID, manifest.EngineString(e.cli.Name), manifest.EngineString(fe.cli.Name), fail)
	return fe, true
}
