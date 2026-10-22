package evals

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// TestLive runs every fixture through both arms against a real CLI and
// appends the results to the scorecard. It spends real tokens, so it runs
// only under DHI_EVAL=1 (`make eval`). Knobs:
//
//   - DHI_EVAL_CLI: registry name (default claude)
//   - DHI_EVAL_MODEL: model for both arms (default: the CLI's own)
//   - DHI_EVAL_TASK: run one fixture only
//   - DHI_EVAL_REPEAT: runs per arm (default 1 — an anecdote; use 3+ for a claim)
//   - DHI_EVAL_OUT: scorecard path (default <repo>/.dhi/evals/scorecard.jsonl)
//   - DHI_EVAL_KEEP=1: keep each run's directory (transcripts, bus) and log it
//   - DHI_EVAL_SKILL: give the DHI agent one built-in skill and report
//     whether it read or activated it (ADR-0043)
//   - DHI_EVAL_ARM: run one arm only (raw or dhi)
//
// Arm order alternates per run so neither arm always inherits the CLI's
// warm prompt cache.
func TestLive(t *testing.T) {
	if os.Getenv("DHI_EVAL") != "1" {
		t.Skip("live evals spend tokens; set DHI_EVAL=1 (make eval)")
	}
	cli := os.Getenv("DHI_EVAL_CLI")
	if cli == "" {
		cli = "claude"
	}
	out := os.Getenv("DHI_EVAL_OUT")
	if out == "" {
		out = filepath.Join("..", "..", ".dhi", "evals", "scorecard.jsonl")
	}
	repeat := 1
	if n, err := strconv.Atoi(os.Getenv("DHI_EVAL_REPEAT")); err == nil && n > 0 {
		repeat = n
	}
	c := Config{
		CLI: cli, Model: os.Getenv("DHI_EVAL_MODEL"),
		Look: exec.LookPath,
		// The app's base env (the host env):
		// claude on macOS needs USER to find its Keychain login, so a
		// stripped env would fail both arms as "Not logged in".
		Env:     os.Environ(),
		Timeout: 15 * time.Minute,
		Retries: 1,
	}
	tasks, err := LoadTasks("testdata/tasks")
	if err != nil {
		t.Fatal(err)
	}
	var results []Result
	turn := 0
	for _, task := range tasks {
		if only := os.Getenv("DHI_EVAL_TASK"); only != "" && only != task.Name {
			continue
		}
		for i := 0; i < repeat; i++ {
			turn++
			for _, run := range ArmOrder(turn) {
				if arm := os.Getenv("DHI_EVAL_ARM"); arm != "" && armName(run) != arm {
					continue
				}
				dir := t.TempDir()
				if os.Getenv("DHI_EVAL_KEEP") == "1" {
					if dir, err = os.MkdirTemp("", "dhi-eval-"+task.Name+"-"); err != nil {
						t.Fatal(err)
					}
				}
				r := run(c, task, dir)
				if os.Getenv("DHI_EVAL_KEEP") == "1" {
					t.Logf("kept %s run: %s", r.Arm, dir)
				}
				t.Logf("%-16s %-3s ok=%-5v tokens=%d+%d cached=%d written=%d cost=$%.4f attempts=%d asks=%d rounds=%d/%d %s %s %v",
					r.Task, r.Arm, r.OK, r.TokensIn, r.TokensOut, r.CacheRead, r.CacheWrite, r.CostUSD, r.Attempts, r.Asks,
					r.VerifyRounds, task.ExpectRounds, r.Verify, r.Err, r.Unmet)
				if r.SkillUse != "" {
					t.Logf("%-16s %-3s skill %s: %s", r.Task, r.Arm, os.Getenv("DHI_EVAL_SKILL"), r.SkillUse)
				}
				results = append(results, r)
			}
		}
	}
	if err := AppendScorecard(out, results); err != nil {
		t.Fatal(err)
	}
	t.Logf("scorecard: %s\n%s", out, Table(Summarize(results)))
}

// armName names an arm function ("dhi" or "raw") for DHI_EVAL_ARM.
func armName(run func(Config, Task, string) Result) string {
	if reflect.ValueOf(run).Pointer() == reflect.ValueOf(RunDHI).Pointer() {
		return "dhi"
	}
	return "raw"
}
