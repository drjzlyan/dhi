// Package budget caps agent spend (F-066, ADR-0031). Every finished run
// appends one line to a workspace spend ledger (.dhi/spend.jsonl) — card
// or not, so chat turns count too — and before each spawn the runtime
// tallies the ledger against the configured limits. A breach refuses
// the spawn with a notice that names the limit; nothing is cut off
// mid-run (a CLI runs to completion, so limits bind between spawns).
package budget

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/drjzlyan/dhi/internal/jsonl"
)

// Limits are the caps; 0 = no limit.
type Limits struct {
	TaskUSD         float64
	TaskTokens      int
	AgentDayUSD     float64
	WorkspaceDayUSD float64
}

// None reports whether no limit is set (the ledger is still written).
func (l Limits) None() bool { return l == Limits{} }

// Entry is one run's spend.
type Entry struct {
	At      time.Time `json:"at"`
	Agent   string    `json:"agent"`
	Task    string    `json:"task,omitempty"` // card slug; "" for card-less turns
	Tokens  int       `json:"tokens"`         // whole prompt + output (unknown counts as 0)
	CostUSD float64   `json:"cost_usd"`
}

// Ledger is the append-only spend log.
type Ledger struct{ path string }

// Open returns the ledger under wsRoot.
func Open(wsRoot string) *Ledger {
	return &Ledger{path: filepath.Join(wsRoot, ".dhi", "spend.jsonl")}
}

// Append records one run.
func (l *Ledger) Append(e Entry) error {
	if err := jsonl.Append(l.path, e); err != nil {
		return fmt.Errorf("budget: ledger: %w", err)
	}
	return nil
}

// Entries reads the whole ledger (missing = empty).
func (l *Ledger) Entries() ([]Entry, error) {
	es, err := jsonl.ReadAll[Entry](l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("budget: ledger: %w", err)
	}
	return es, nil
}

// Spent is what one prospective spawn is charged against.
type Spent struct {
	TaskUSD         float64
	TaskTokens      int
	AgentDayUSD     float64
	WorkspaceDayUSD float64
}

// Tally sums the ledger for agent / task, counting day totals from
// dayStart (local midnight, chosen by the caller).
func Tally(entries []Entry, agent, task string, dayStart time.Time) Spent {
	var s Spent
	for _, e := range entries {
		if task != "" && e.Task == task {
			s.TaskUSD += e.CostUSD
			s.TaskTokens += e.Tokens
		}
		if e.At.Before(dayStart) {
			continue
		}
		s.WorkspaceDayUSD += e.CostUSD
		if e.Agent == agent {
			s.AgentDayUSD += e.CostUSD
		}
	}
	return s
}

// Check names the first limit spent has reached, with the setting that
// raises it; ok is true when the spawn may proceed. Task limits apply
// only to card-bound turns (hasTask).
func Check(l Limits, s Spent, hasTask bool) (breach string, ok bool) {
	switch {
	case hasTask && l.TaskUSD > 0 && s.TaskUSD >= l.TaskUSD:
		return fmt.Sprintf("this task has spent $%.2f of its $%.2f budget (budgets.task_usd)", s.TaskUSD, l.TaskUSD), false
	case hasTask && l.TaskTokens > 0 && s.TaskTokens >= l.TaskTokens:
		return fmt.Sprintf("this task has used %d of its %d-token budget (budgets.task_tokens)", s.TaskTokens, l.TaskTokens), false
	case l.AgentDayUSD > 0 && s.AgentDayUSD >= l.AgentDayUSD:
		return fmt.Sprintf("this agent has spent $%.2f of its $%.2f daily budget (budgets.agent_day_usd)", s.AgentDayUSD, l.AgentDayUSD), false
	case l.WorkspaceDayUSD > 0 && s.WorkspaceDayUSD >= l.WorkspaceDayUSD:
		return fmt.Sprintf("the workspace has spent $%.2f of its $%.2f daily budget (budgets.workspace_day_usd)", s.WorkspaceDayUSD, l.WorkspaceDayUSD), false
	}
	return "", true
}

// DayStart is local midnight of t.
func DayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
