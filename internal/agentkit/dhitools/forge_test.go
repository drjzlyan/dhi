package dhitools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
)

// F-069: an agent proposes a tool; it is not callable until approved;
// once approved every agent gets custom__<name> at its declared scope.
func TestToolProposeApproveCall(t *testing.T) {
	f, m := newFixture(t) // no allowlist at all: forge tools are harness tools
	store := forge.Open(f.ws.Root)
	fr := &fakeRunner{out: "PASS\n"}
	deps := Deps{Agent: m, WS: f.ws, Bus: f.bus, Channel: "#general", Workdir: "/wd",
		Run: fr, Forge: store, Approvals: f.approvals}

	out, isErr := call(deps.Handler(), t, "tool_propose", `{"name":"race_test","description":"Run one package's tests with -race",
		"scope":"exec","inputs":[{"name":"pkg","type":"string","required":true}],"steps":[["go","test","-race","{{pkg}}"]],
		"rationale":"ran it in 4 of 6 tasks"}`)
	if isErr || !strings.Contains(out, "proposed race_test v1") {
		t.Fatalf("propose = %q", out)
	}
	if slices.Contains(toolNames(deps.Handler()), "custom__race_test") {
		t.Fatal("a pending tool must not be served")
	}
	hist := f.bus.History("#general", 0)
	if len(hist) != 1 || !strings.HasPrefix(hist[0].Text, "@you I proposed a tool `race_test`") {
		t.Fatalf("the proposal must @mention the human: %+v", hist)
	}

	if _, err := store.Approve("race_test", bus.Human); err != nil {
		t.Fatal(err)
	}
	h := deps.Handler()
	if !slices.Contains(toolNames(h), "custom__race_test") {
		t.Fatalf("approved tool not served: %v", toolNames(h))
	}
	// exec scope asks (outside the envelope); approve it.
	f.approvals.OnRequest = func(ap *tools.Approval) { go f.approvals.Resolve(ap.ID, true) }
	out, isErr, err := h.CallTool(context.Background(), "custom__race_test", json.RawMessage(`{"pkg":"./money"}`))
	if err != nil || isErr || !strings.Contains(out, "$ go test -race ./money\nPASS") {
		t.Fatalf("call = %q %v %v", out, isErr, err)
	}
	if strings.Join(fr.argv, " ") != "go test -race ./money" || fr.dir != "/wd" || fr.network {
		t.Fatalf("ran %v in %s (net %v)", fr.argv, fr.dir, fr.network)
	}
	if store.Usages()["race_test"].Calls != 1 {
		t.Fatal("use not counted")
	}
	if out, isErr := call(h, t, "custom__race_test", `{"pkg":"a","x":1}`); !isErr || !strings.Contains(out, "unknown argument") {
		t.Fatalf("bad args must refuse before running: %q", out)
	}
}

func TestToolProposeRefusesUnsafe(t *testing.T) {
	f, m := newFixture(t)
	h := Deps{Agent: m, WS: f.ws, Forge: forge.Open(f.ws.Root)}.Handler()
	for _, args := range []string{
		`{"name":"fetch","description":"d","scope":"network","steps":[["go","test"]],"rationale":"r"}`,
		`{"name":"pwn","description":"d","scope":"exec","steps":[["curl","x"]],"rationale":"r"}`,
		`{"name":"sh_it","description":"d","scope":"exec","steps":[["go","run","x.go"]],"rationale":"r"}`,
	} {
		if out, isErr := call(h, t, "tool_propose", args); !isErr {
			t.Errorf("%s: want a refusal, got %q", args, out)
		}
	}
}

// F-071: task_create holds a brief behind open dependencies, otherwise
// hands it off and binds the card to it.
func TestTaskCreateHoldsAndBinds(t *testing.T) {
	f, m := newFixture(t, "task_create")
	f.approvals.OnRequest = func(ap *tools.Approval) { go f.approvals.Resolve(ap.ID, true) }
	h := Deps{Agent: m, WS: f.ws, Tasks: f.tasks, Bus: f.bus, Approvals: f.approvals}.Handler()
	if out, isErr := call(h, t, "task_create", `{"slug":"schema","title":"Schema","assignee":"dev"}`); isErr {
		t.Fatal(out)
	}
	card, _ := f.tasks.Get("schema")
	if card.ThreadChannel != "dm:dev" || card.ThreadID == 0 {
		t.Fatalf("handed-off card must be bound to its brief: %+v", card)
	}
	out, isErr := call(h, t, "task_create", `{"slug":"api","title":"API","assignee":"dev","depends_on":["schema"]}`)
	if isErr || !strings.Contains(out, "held until schema are done") {
		t.Fatalf("create = %q", out)
	}
	if api, _ := f.tasks.Get("api"); !api.Held || api.ThreadID != 0 {
		t.Fatalf("a blocked card must be held, not handed off: %+v", api)
	}
	if out, isErr := call(h, t, "task_create", `{"slug":"loop","title":"x","depends_on":["loop"]}`); !isErr {
		t.Fatalf("self-dependency must refuse: %q", out)
	}
}
