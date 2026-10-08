package tasks

import (
	"fmt"
	"sort"
)

// Rollup is pure arithmetic over run records (F-014 §Part A): counts and
// token/cost sums. Slices keyed off a record set — never persisted.
type Rollup struct {
	Runs    int
	OK      int // status ok
	Fail    int // status error
	Timeout int // status timeout
	ExitMax int // largest recorded exit code (0 if none)

	TokensIn   int // sums over runs with a known (-1 excluded) count
	TokensOut  int
	TokRuns    int  // runs contributing a known token count
	TokPartial bool // any run in the set carried -1 token counts

	CostSum     float64 // over costed runs only
	Costed      int     // runs with HasCost
	CostPartial bool    // any run in the set was cost-less (cost:false)
}

// RollupRuns folds a run slice into a Rollup. A run with cost=false is
// counted in Runs/tokens-known columns but excluded from the cost sum
// and flags the sum "partial" (F-014 §Part A).
func RollupRuns(runs []Run) Rollup {
	var rl Rollup
	for _, r := range runs {
		rl.Runs++
		switch r.Status {
		case RunOK:
			rl.OK++
		case RunError:
			rl.Fail++
		case RunTimeout:
			rl.Timeout++
		default:
			rl.Fail++ // untracked statuses are failures to surface
		}
		if r.Exit > rl.ExitMax {
			rl.ExitMax = r.Exit
		}
		in, out := r.TokensIn, r.TokensOut
		switch {
		case in >= 0 && out >= 0:
			rl.TokensIn += in
			rl.TokensOut += out
			rl.TokRuns++
		default:
			rl.TokPartial = true
		}
		if r.HasCost {
			rl.CostSum += r.CostUSD
			rl.Costed++
		} else {
			rl.CostPartial = true
		}
	}
	return rl
}

// TaskRollup folds one card's run history.
func (s *Store) TaskRollup(slug string) (Rollup, error) {
	t, ok := s.Get(slug)
	if !ok {
		return Rollup{}, fmt.Errorf("tasks: unknown task %q", slug)
	}
	return RollupRuns(t.Runs), nil
}

// AgentRollup folds every run recorded by id across all cards.
func (s *Store) AgentRollup(id string) Rollup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []Run
	for _, t := range s.tasks {
		for _, r := range t.Runs {
			if r.Agent == id {
				all = append(all, r)
			}
		}
	}
	return RollupRuns(all)
}

// NewestRun returns the most recently finished run, if any.
func (t *Task) NewestRun() (Run, bool) {
	var best Run
	var ok bool
	for _, r := range t.Runs {
		if !ok || r.Finished.After(best.Finished) {
			best, ok = r, true
		}
	}
	return best, ok
}

// AgentRuns collects every run recorded by id, newest first.
func (s *Store) AgentRuns(id string) []Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Run
	for _, t := range s.tasks {
		for _, r := range t.Runs {
			if r.Agent == id {
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Started.After(out[j].Started)
	})
	return out
}

// ModelRollup is one model's folded run set (F-014 §Part A: cost per
// model). The embedded Rollup carries the counts/sums; Model is the
// manifest model tag ("" = the engine's default/unknown).
type ModelRollup struct {
	Model string
	Rollup
}

// RollupByModel folds a run slice per runtime model, most-used first
// (ties broken by model name for determinism). Pure arithmetic over the
// records — no persistence.
func RollupByModel(runs []Run) []ModelRollup {
	groups := map[string][]Run{}
	for _, r := range runs {
		groups[r.Model] = append(groups[r.Model], r)
	}
	out := make([]ModelRollup, 0, len(groups))
	for m, rs := range groups {
		out = append(out, ModelRollup{Model: m, Rollup: RollupRuns(rs)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runs != out[j].Runs {
			return out[i].Runs > out[j].Runs
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// Line renders one model breakdown row (F-014 §Part B).
func (m ModelRollup) Line() string {
	name := m.Model
	if name == "" {
		name = "(default model)"
	}
	return fmt.Sprintf("%s · %d runs · %s · %d fail", name, m.Runs, m.CostText(), m.Fail)
}

// CostText renders the F-014 cost column: "n/a" when nothing is costed,
// "cost partial" when any run is cost-less, else the exact dollar sum.
func (r Rollup) CostText() string {
	switch {
	case r.Runs == 0:
		return "n/a"
	case r.CostPartial:
		return "cost partial"
	default:
		return fmt.Sprintf("$%.4g", r.CostSum)
	}
}

// TokensText renders the F-014 tokens column, marking partial input
// when an unknown row was excluded.
func (r Rollup) TokensText() string {
	switch {
	case r.Runs == 0:
		return "n/a"
	case r.TokRuns == 0:
		return "tokens n/a"
	default:
		s := fmt.Sprintf("%s in / %s out", humanTokens(r.TokensIn), humanTokens(r.TokensOut))
		if r.TokPartial {
			s += " partial"
		}
		return s
	}
}

// Summary renders the INSPECT totals line (F-014 §Part B):
// runs · ok/fail/timeout · tokens · cost.
func (r Rollup) Summary() string {
	return fmt.Sprintf("%d runs · %d ok · %d fail · %d timeout · %s · %s",
		r.Runs, r.OK, r.Fail, r.Timeout, r.TokensText(), r.CostText())
}

// humanTokens renders 1234 → "1.2k"; small counts stay verbatim.
func humanTokens(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// DurationText renders a run's wall-clock span compactly (e.g. "1.2s").
func DurationText(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1e3)
	default:
		return fmt.Sprintf("%.1fm", float64(ms)/60e3)
	}
}
