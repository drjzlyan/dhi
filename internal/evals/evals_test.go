package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// doneStub is a claude fixture that does the offline task's work in its
// cwd and reports usage with a prompt-cache read.
const doneStub = `#!/bin/sh
mkdir -p app && printf 'done\n' > app/out.txt
printf '%s\n' '{"type":"system","subtype":"init"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"wrote it","total_cost_usd":0.002,"usage":{"input_tokens":40,"cache_read_input_tokens":360,"output_tokens":10}}'
`

// idleStub succeeds without doing the work.
const idleStub = `#!/bin/sh
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"nothing","usage":{"input_tokens":5,"output_tokens":1}}'
`

func stubConfig(t *testing.T, script string) Config {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Config{
		CLI: "claude",
		Look: func(name string) (string, error) {
			p := filepath.Join(bin, name)
			_, err := os.Stat(p)
			return p, err
		},
		Env:     []string{"PATH=" + bin + ":/usr/bin:/bin"},
		Timeout: time.Minute,
		Now:     func() time.Time { return time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC) },
	}
}

func offlineTask(t *testing.T) Task {
	t.Helper()
	ts, err := LoadTasks("testdata/offline")
	if err != nil || len(ts) != 1 {
		t.Fatalf("LoadTasks = %v, %v", ts, err)
	}
	return ts[0]
}

func TestBothArmsScoreSuccess(t *testing.T) {
	c := stubConfig(t, doneStub)
	task := offlineTask(t)
	for _, run := range []func(Config, Task, string) Result{RunRaw, RunDHI} {
		r := run(c, task, t.TempDir())
		if !r.OK || r.Err != "" || len(r.Unmet) > 0 {
			t.Fatalf("%s arm: %+v", r.Arm, r)
		}
		if r.TokensIn != 40 || r.CacheRead != 360 || r.Attempts != 1 || !r.HasCost {
			t.Fatalf("%s arm usage: %+v", r.Arm, r)
		}
	}
}

func TestUnmetExpectationFails(t *testing.T) {
	c := stubConfig(t, idleStub)
	r := RunDHI(c, offlineTask(t), t.TempDir())
	if r.OK || len(r.Unmet) != 1 || !strings.Contains(r.Unmet[0], "app/out.txt missing") {
		t.Fatalf("result = %+v", r)
	}
}

func TestOverBudget(t *testing.T) {
	task := offlineTask(t)
	task.BudgetTokens = 100
	r := score(task, t.TempDir(), Result{TokensIn: 40, CacheRead: 360, TokensOut: 10})
	if !r.OverBudget {
		t.Fatalf("410 tokens against a 100 budget must be over: %+v", r)
	}
}

func TestLoadTasksRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "name = \"x\"\nprompt = \"p\"\nbudget = 3\n[[expect]]\nfile = \"a\"\n"
	if err := os.WriteFile(filepath.Join(dir, "x", "task.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTasks(dir); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("unknown key must refuse by name, got %v", err)
	}
}

// The shipped live fixtures must load and stage cleanly.
func TestLiveFixturesLoad(t *testing.T) {
	ts, err := LoadTasks("testdata/tasks")
	if err != nil || len(ts) < 2 {
		t.Fatalf("LoadTasks = %d tasks, %v", len(ts), err)
	}
	for _, task := range ts {
		root := t.TempDir()
		if err := task.Stage(root); err != nil {
			t.Fatalf("%s: stage: %v", task.Name, err)
		}
		if unmet := task.Check(root); len(unmet) == 0 {
			t.Fatalf("%s: the seed must not already satisfy the task", task.Name)
		}
	}
}

func TestScorecardAndSummary(t *testing.T) {
	results := []Result{
		{Task: "a", Arm: "raw", OK: true, Attempts: 1, TokensIn: 1000, TokensOut: 100, WallMs: 2000},
		{Task: "b", Arm: "raw", OK: false, Attempts: 1, TokensIn: 900, TokensOut: 90, WallMs: 1000},
		{Task: "a", Arm: "dhi", OK: true, Attempts: 1, TokensIn: 200, CacheRead: 800, TokensOut: 100, WallMs: 3000},
		{Task: "b", Arm: "dhi", OK: true, Attempts: 2, Asks: 1, TokensIn: 300, CacheRead: 700, TokensOut: 50, WallMs: 1000},
	}
	path := filepath.Join(t.TempDir(), "evals", "scorecard.jsonl")
	if err := AppendScorecard(path, results); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "\n"); n != 4 {
		t.Fatalf("scorecard rows = %d", n)
	}
	sums := Summarize(results)
	if len(sums) != 2 || sums[0].Arm != "raw" || sums[1].Arm != "dhi" {
		t.Fatalf("summaries = %+v", sums)
	}
	raw, dhi := sums[0], sums[1]
	if raw.OK != 1 || raw.TokensPerOK != 1100 || raw.CacheShare != 0 {
		t.Fatalf("raw = %+v", raw)
	}
	if dhi.OK != 2 || dhi.TokensPerOK != 1075 || dhi.CacheShare != 75 || dhi.Attempts != 3 || dhi.Asks != 1 {
		t.Fatalf("dhi = %+v", dhi)
	}
	table := Table(sums)
	if !strings.Contains(table, "raw    1/2") || !strings.Contains(table, "75%") {
		t.Fatalf("table:\n%s", table)
	}
}

func TestArmOrderAlternates(t *testing.T) {
	c := stubConfig(t, doneStub)
	task := offlineTask(t)
	first := func(turn int) string { return ArmOrder(turn)[0](c, task, t.TempDir()).Arm }
	if first(1) != "raw" || first(2) != "dhi" {
		t.Fatalf("order = %s then %s, want raw then dhi", first(1), first(2))
	}
}
