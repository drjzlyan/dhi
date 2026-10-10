// Package evals measures DHI's agent harness against the bare host CLI
// (F-065): the same fixture task runs once through the raw CLI headless
// and once through DHI's runtime, and each run is scored on outcome,
// tokens, prompt-cache reads, cost, wall time, attempts and approval
// asks. Results append to a JSONL scorecard so harness changes are judged
// by numbers, not by feel. Offline tests drive both arms with stub CLIs;
// live runs (real tokens) happen only under DHI_EVAL=1 (`make eval`).
package evals

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/jsonl"
)

// Member is the workspace member a fixture's seed tree is copied into.
// Prompts and expectations address files as "app/<rel-path>".
const Member = "app"

// Task is one fixture: a prompt, a seed tree and what must hold after.
type Task struct {
	Name         string   `toml:"name"`
	Prompt       string   `toml:"prompt"`
	BudgetTokens int      `toml:"budget_tokens"` // 0 = no budget
	Expect       []Expect `toml:"expect"`

	dir string // fixture directory (holds seed/)
}

// Expect is one post-condition on a workspace-relative file.
type Expect struct {
	File     string `toml:"file"`
	Contains string `toml:"contains"` // "" = the file only has to exist
	Absent   bool   `toml:"absent"`   // the file must NOT exist
}

// LoadTasks reads every <dir>/<name>/task.toml, sorted by name.
func LoadTasks(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("evals: %w", err)
	}
	var out []Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var t Task
		p := filepath.Join(dir, e.Name(), "task.toml")
		md, err := toml.DecodeFile(p, &t)
		if err != nil {
			return nil, fmt.Errorf("evals: %s: %w", p, err)
		}
		if und := md.Undecoded(); len(und) > 0 {
			return nil, fmt.Errorf("evals: %s: unknown key %q", p, und[0].String())
		}
		if t.Name == "" || strings.TrimSpace(t.Prompt) == "" || len(t.Expect) == 0 {
			return nil, fmt.Errorf("evals: %s: name, prompt and at least one expect are required", p)
		}
		t.dir = filepath.Join(dir, e.Name())
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Stage builds a fresh run directory: <root>/app holds a copy of the
// task's seed tree.
func (t Task) Stage(root string) error {
	dst := filepath.Join(root, Member)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	seed := filepath.Join(t.dir, "seed")
	if _, err := os.Stat(seed); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(seed, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(seed, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// Check evaluates the expectations against root; it returns the unmet
// ones as readable lines (empty = success).
func (t Task) Check(root string) []string {
	var unmet []string
	for _, x := range t.Expect {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(x.File)))
		switch {
		case x.Absent && err == nil:
			unmet = append(unmet, x.File+" must not exist")
		case x.Absent:
		case err != nil:
			unmet = append(unmet, x.File+" missing")
		case x.Contains != "" && !strings.Contains(string(b), x.Contains):
			unmet = append(unmet, fmt.Sprintf("%s lacks %q", x.File, x.Contains))
		}
	}
	return unmet
}

// Result is one scored run of one task through one arm.
type Result struct {
	At       time.Time `json:"at"`
	Task     string    `json:"task"`
	Arm      string    `json:"arm"` // "raw" | "dhi"
	CLI      string    `json:"cli"`
	Model    string    `json:"model,omitempty"`
	OK       bool      `json:"ok"`
	Unmet    []string  `json:"unmet,omitempty"`
	Err      string    `json:"err,omitempty"`
	Attempts int       `json:"attempts"`
	Asks     int       `json:"asks"` // approvals requested (auto-allowed in evals)

	TokensIn   int     `json:"tokens_in"` // -1 = unknown
	TokensOut  int     `json:"tokens_out"`
	CacheRead  int     `json:"cache_read"`
	CacheWrite int     `json:"cache_write"`
	CostUSD    float64 `json:"cost_usd"`
	HasCost    bool    `json:"has_cost"`
	WallMs     int64   `json:"wall_ms"`

	OverBudget bool `json:"over_budget,omitempty"`
}

// promptTokens is the whole prompt: fresh input plus cache reads and
// writes (the clirun.Usage invariant), so arms with different cache
// behaviour compare on the same footing.
func (r Result) promptTokens() int { return r.TokensIn + r.CacheRead + r.CacheWrite }

// score finishes a result: success, budget, unmet expectations.
func score(t Task, root string, r Result) Result {
	r.Unmet = t.Check(root)
	r.OK = r.Err == "" && len(r.Unmet) == 0
	if t.BudgetTokens > 0 && r.TokensIn >= 0 {
		r.OverBudget = r.promptTokens()+r.TokensOut > t.BudgetTokens
	}
	return r
}

// AppendScorecard appends results to the JSONL scorecard at path.
func AppendScorecard(path string, results []Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	for _, r := range results {
		if err := jsonl.Append(path, r); err != nil {
			return fmt.Errorf("evals: scorecard: %w", err)
		}
	}
	return nil
}

// ArmSummary aggregates one arm's results.
type ArmSummary struct {
	Arm                string
	Runs, OK           int
	TokensPerOK        int // (whole prompt + out) per success; 0 when none
	CacheShare         int // percent of the whole prompt served from cache
	CostUSD            float64
	Attempts, Asks     int
	MeanWallMs         int64
	tokens, in, cached int // in = whole prompt tokens
}

// Summarize folds results per arm, raw first.
func Summarize(results []Result) []ArmSummary {
	by := map[string]*ArmSummary{}
	for _, r := range results {
		s := by[r.Arm]
		if s == nil {
			s = &ArmSummary{Arm: r.Arm}
			by[r.Arm] = s
		}
		s.Runs++
		s.Attempts += r.Attempts
		s.Asks += r.Asks
		s.CostUSD += r.CostUSD
		s.MeanWallMs += r.WallMs
		if r.TokensIn >= 0 {
			s.in += r.promptTokens()
			s.cached += r.CacheRead
		}
		if r.OK {
			s.OK++
			if r.TokensIn >= 0 {
				s.tokens += r.promptTokens() + r.TokensOut
			}
		}
	}
	var out []ArmSummary
	for _, s := range by {
		if s.OK > 0 {
			s.TokensPerOK = s.tokens / s.OK
		}
		if s.in > 0 {
			s.CacheShare = s.cached * 100 / s.in
		}
		s.MeanWallMs /= int64(s.Runs)
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Arm > out[j].Arm }) // raw, then dhi
	return out
}

// Table renders summaries as aligned text for `make eval` output.
func Table(sums []ArmSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-4s %7s %12s %7s %9s %8s %5s %8s\n", "arm", "success", "tokens/ok", "cached", "cost", "attempts", "asks", "wall")
	for _, s := range sums {
		fmt.Fprintf(&b, "%-4s %3d/%-3d %12d %6d%% %9s %8d %5d %7.1fs\n",
			s.Arm, s.OK, s.Runs, s.TokensPerOK, s.CacheShare, fmt.Sprintf("$%.4f", s.CostUSD),
			s.Attempts, s.Asks, float64(s.MeanWallMs)/1e3)
	}
	return b.String()
}
