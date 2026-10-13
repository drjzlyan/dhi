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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/drjzlyan/dhi/internal/jsonl"
)

// Member is the workspace member a fixture's seed tree is copied into.
// Prompts and expectations address files as "app/<rel-path>".
const Member = "app"

// Task is one fixture: a prompt, a seed tree and what must hold after.
type Task struct {
	Name   string `toml:"name"`
	Prompt string `toml:"prompt"`
	// Followups are later turns of the same conversation (F-066): each
	// arm sends them after the opening prompt, continuing its session —
	// the raw CLI by resuming its own session id, DHI by replying in the
	// task's thread.
	Followups    []string `toml:"followups"`
	BudgetTokens int      `toml:"budget_tokens"` // 0 = no budget
	Expect       []Expect `toml:"expect"`
	// Verify is the DHI arm's verify command for the member (F-077,
	// through the F-076 workspace override): a fixture can check more
	// than the agent is shown, so the first fix fails verification and
	// the retry loop has to work. Empty = DHI detects it.
	Verify []string `toml:"verify"`
	// MaxIterations caps DHI's verify rounds (0 = the default 3).
	MaxIterations int `toml:"max_iterations"`
	// ExpectRounds is the verify rounds the fixture is built to need;
	// reported against the actual rounds, never scored.
	ExpectRounds int `toml:"expect_verify_rounds"`

	dir string // fixture directory (holds seed/)
}

// Expect is one post-condition on a workspace-relative file.
type Expect struct {
	File     string `toml:"file"`
	Contains string `toml:"contains"` // "" = the file only has to exist
	Absent   bool   `toml:"absent"`   // the file must NOT exist
	// Run is a command that must exit 0 in the member dir (e.g.
	// ["go", "test", "./..."]); resolved on PATH. This is a dev-only
	// eval check, not product code — DHI itself never shells out here.
	Run []string `toml:"run"`
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
		for _, x := range t.Expect {
			if (x.File == "") == (len(x.Run) == 0) {
				return nil, fmt.Errorf("evals: %s: each expect names exactly one of file or run", p)
			}
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
// task's seed tree as a git repository with one commit — a realistic
// member, so a workflow's branch / commit steps have something to act on.
func (t Task) Stage(root string) error {
	dst := filepath.Join(root, Member)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if err := t.copySeed(dst); err != nil {
		return err
	}
	return initRepo(dst)
}

// initRepo commits dst's tree on main through go-git (hermetic: no host
// git binary).
func initRepo(dir string) error {
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	if err != nil {
		return fmt.Errorf("evals: git init: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("evals: git add: %w", err)
	}
	sig := &object.Signature{Name: "DHI Evals", Email: "evals@dhi.invalid", When: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	_, err = wt.Commit("seed", &git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true})
	if err != nil {
		return fmt.Errorf("evals: git commit: %w", err)
	}
	return nil
}

func (t Task) copySeed(dst string) error {
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
func (t Task) Check(root string) []string { return t.CheckIn(root, filepath.Join(root, Member)) }

// CheckIn is Check with the member's files read from memberDir — a task
// worktree, where DHI's arm does its work (F-067) — instead of
// <root>/app.
func (t Task) CheckIn(root, memberDir string) []string {
	var unmet []string
	for _, x := range t.Expect {
		if len(x.Run) > 0 {
			cmd := exec.Command(x.Run[0], x.Run[1:]...)
			cmd.Dir = memberDir
			if out, err := cmd.CombinedOutput(); err != nil {
				unmet = append(unmet, fmt.Sprintf("%s failed: %s", strings.Join(x.Run, " "), lastLine(string(out))))
			}
			continue
		}
		b, err := os.ReadFile(memberPath(root, memberDir, x.File))
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

// memberPath maps an expectation path ("app/x.go") into memberDir.
func memberPath(root, memberDir, file string) string {
	if rel, ok := strings.CutPrefix(filepath.ToSlash(file), Member+"/"); ok {
		return filepath.Join(memberDir, filepath.FromSlash(rel))
	}
	return filepath.Join(root, filepath.FromSlash(file))
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
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

	// DHI's goal loop (F-077): verify rounds run and the last verdict
	// (pass / fail / stuck / env / "" = no verify).
	VerifyRounds int    `json:"verify_rounds,omitempty"`
	Verify       string `json:"verify,omitempty"`
}

// promptTokens is the whole prompt: fresh input plus cache reads and
// writes (the clirun.Usage invariant), so arms with different cache
// behaviour compare on the same footing.
func (r Result) promptTokens() int { return r.TokensIn + r.CacheRead + r.CacheWrite }

// score finishes a result: success, budget, unmet expectations.
func score(t Task, root string, r Result) Result {
	return scoreIn(t, root, filepath.Join(root, Member), r)
}

// scoreIn scores against the member files in memberDir.
func scoreIn(t Task, root, memberDir string, r Result) Result {
	r.Unmet = t.CheckIn(root, memberDir)
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
	VerifyRounds       int
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
		s.VerifyRounds += r.VerifyRounds
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
	fmt.Fprintf(&b, "%-4s %7s %12s %7s %9s %8s %5s %6s %8s\n", "arm", "success", "tokens/ok", "cached", "cost", "attempts", "asks", "rounds", "wall")
	for _, s := range sums {
		fmt.Fprintf(&b, "%-4s %3d/%-3d %12d %6d%% %9s %8d %5d %6d %7.1fs\n",
			s.Arm, s.OK, s.Runs, s.TokensPerOK, s.CacheShare, fmt.Sprintf("$%.4f", s.CostUSD),
			s.Attempts, s.Asks, s.VerifyRounds, float64(s.MeanWallMs)/1e3)
	}
	return b.String()
}
