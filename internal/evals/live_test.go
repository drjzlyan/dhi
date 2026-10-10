package evals

import (
	"os"
	"os/exec"
	"path/filepath"
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
		// The app's base env (toolchain.Manager.Env(nil) = the host env):
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
				r := run(c, task, t.TempDir())
				t.Logf("%-14s %-3s ok=%-5v tokens=%d+%d cached=%d written=%d cost=$%.4f attempts=%d asks=%d %s %v",
					r.Task, r.Arm, r.OK, r.TokensIn, r.TokensOut, r.CacheRead, r.CacheWrite, r.CostUSD, r.Attempts, r.Asks, r.Err, r.Unmet)
				results = append(results, r)
			}
		}
	}
	if err := AppendScorecard(out, results); err != nil {
		t.Fatal(err)
	}
	t.Logf("scorecard: %s\n%s", out, Table(Summarize(results)))
}
