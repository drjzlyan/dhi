package trace

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/tasks"
)

var t0 = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

func sample() Input {
	tk := tasks.Task{Slug: "login", Title: "Fix login", Status: tasks.Active, Assignee: "forge",
		Runs: []tasks.Run{
			{ID: "run-1", Agent: "forge", Attempt: 0, Started: t0.Add(time.Minute), Finished: t0.Add(3 * time.Minute),
				Status: tasks.RunError, Error: "stalled", Transcript: "r1.jsonl", TokensIn: -1, TokensOut: -1},
			{ID: "run-2", Agent: "forge", Attempt: 1, Started: t0.Add(4 * time.Minute), Finished: t0.Add(5 * time.Minute),
				Status: tasks.RunOK, Transcript: "legacy.jsonl", TokensIn: 10, TokensOut: 20, HasCost: true, CostUSD: 0.12},
			{ID: "run-3", Agent: "forge", Started: t0.Add(6 * time.Minute), Transcript: "gone.jsonl"},
		},
		Comments: []tasks.Comment{{Author: "you", Text: "please hurry", At: t0.Add(30 * time.Second)}},
		Activity: []tasks.Activity{{Kind: tasks.ActAssignee, To: "forge", Actor: "you", At: t0.Add(10 * time.Second)}},
	}
	return Input{
		Task: tk,
		Thread: []bus.Message{
			{ID: 1, Author: "you", Text: "@forge fix login", At: t0},
			{ID: 2, Author: "forge", Kind: bus.KindStream, Text: "bash: go test ./...", At: t0.Add(2 * time.Minute)},
			{ID: 3, Author: "forge", Kind: bus.KindStream, Text: "retrying (attempt 1/3) in 30s… (stall)", At: t0.Add(3*time.Minute + time.Second)},
			{ID: 4, Author: "forge", Kind: bus.KindReply, Text: "Fixed.", At: t0.Add(5*time.Minute + time.Second)},
		},
		Read: func(p string) ([]Record, error) {
			switch p {
			case "r1.jsonl":
				return []Record{
					{Kind: "prompt", Detail: "prompt 10 chars", At: t0.Add(time.Minute)},
					{Kind: "command", Detail: "bash: go test ./...", At: t0.Add(2 * time.Minute)},
				}, nil
			case "legacy.jsonl":
				return []Record{{Kind: "progress", Detail: "looking"}, {Kind: "final", Detail: "{...}"}}, nil
			}
			return nil, errors.New("missing")
		},
	}
}

func TestBuildMergesInTimeOrder(t *testing.T) {
	tr := Build(sample())
	var kinds []string
	for i, e := range tr.Events {
		if i > 0 && e.At.Before(tr.Events[i-1].At) {
			t.Fatalf("event %d out of order: %+v", i, e)
		}
		kinds = append(kinds, e.Kind)
	}
	got := strings.Join(kinds, ",")
	want := "message,activity,comment,run,prompt,command,run_end,notice,run,progress,final,run_end,reply,run,run_end"
	if got != want {
		t.Fatalf("kinds\n got %s\nwant %s", got, want)
	}
	if len(tr.Missing) != 1 || tr.Missing[0] != "run-3" {
		t.Fatalf("missing = %v", tr.Missing)
	}
}

func TestFilterByRunKindAndQuery(t *testing.T) {
	tr := Build(sample())
	if evs := tr.Filter("run-2", nil, ""); len(evs) != 4 {
		t.Fatalf("run-2 events = %d", len(evs))
	}
	if evs := tr.Filter("", map[string]bool{KindReply: true, KindMessage: true}, ""); len(evs) != 2 {
		t.Fatalf("conversation = %d", len(evs))
	}
	if evs := tr.Filter("", nil, "RETRYING"); len(evs) != 1 {
		t.Fatalf("query = %d", len(evs))
	}
}

func TestExports(t *testing.T) {
	tr := Build(sample())
	var md bytes.Buffer
	if err := tr.WriteMarkdown(&md, tr.Events); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Trace: login", "| run-2 | forge | 2 | ok | 10/20 | $0.1200 |", "**forge** reply: Fixed."} {
		if !strings.Contains(md.String(), want) {
			t.Fatalf("markdown missing %q:\n%s", want, md.String())
		}
	}
	var jl bytes.Buffer
	if err := tr.WriteJSONL(&jl, tr.Events); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(jl.String()), "\n")
	if len(lines) != len(tr.Events) {
		t.Fatalf("jsonl lines = %d", len(lines))
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil || e.Kind != KindMessage {
		t.Fatalf("first = %+v %v", e, err)
	}
}
