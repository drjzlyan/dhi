package dhitools

import (
	"context"
	"errors"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"strings"
	"testing"
)

// fakeRunner records a command run for the `run` tool.
type fakeRunner struct {
	called bool
	dir    string
	argv   []string
	out    string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, dir string, argv []string) (string, error) {
	f.called = true
	f.dir = dir
	f.argv = argv
	return f.out, f.err
}

func TestMiscToolsServed(t *testing.T) {
	for _, s := range []string{"run", "ask_human"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}

func TestRunExecutesAllowedCommand(t *testing.T) {
	f, m := newFixture(t, "run")
	fr := &fakeRunner{out: "ok\n"}
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Run: fr, Approvals: f.approvals}.Handler()

	resolve := callAsync(h, "run", `{"program":"go","args":["test","./..."]}`, f)
	out, isErr := resolve(t)
	if isErr {
		t.Fatalf("run refused: %s", out)
	}
	if !fr.called || strings.Join(fr.argv, " ") != "go test ./..." || fr.dir != "/tmp/wd" {
		t.Fatalf("runner argv=%v dir=%q called=%v", fr.argv, fr.dir, fr.called)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("run out = %q", out)
	}
}

func TestRunRefusesUnknownProgramBeforeApproval(t *testing.T) {
	f, m := newFixture(t, "run")
	fr := &fakeRunner{}
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Run: fr, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "run", `{"program":"rm","args":["-rf","/"]}`)
	if !isErr || !strings.Contains(out, "not allowed") {
		t.Fatalf("unknown program = %q isErr=%v", out, isErr)
	}
	if len(f.approvals.List()) != 0 || fr.called {
		t.Fatal("disallowed command parked an approval or ran")
	}
}

func TestRunRefusesDisallowedSubcommand(t *testing.T) {
	f, m := newFixture(t, "run")
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Run: &fakeRunner{}, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "run", `{"program":"go","args":["run","./evil"]}`)
	if !isErr || !strings.Contains(out, "subcommand") {
		t.Fatalf("subcommand = %q isErr=%v", out, isErr)
	}
}

func TestRunDeniedDoesNotExecute(t *testing.T) {
	f, m := newFixture(t, "run")
	fr := &fakeRunner{}
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Run: fr, Approvals: f.approvals}.Handler()
	if out, isErr := callDenied(h, t, "run", `{"program":"go","args":["test"]}`, f); !isErr {
		t.Fatalf("denied run must refuse, got %q", out)
	}
	if fr.called {
		t.Fatal("denied run executed the command")
	}
}

func TestRunRefusesWithoutRunner(t *testing.T) {
	f, m := newFixture(t, "run")
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "run", `{"program":"go","args":["test"]}`, f)
	if out, isErr := resolve(t); !isErr || !strings.Contains(out, "runner unavailable") {
		t.Fatalf("no-runner = %q isErr=%v", out, isErr)
	}
}

func TestRunReportsFailureOutput(t *testing.T) {
	f, m := newFixture(t, "run")
	fr := &fakeRunner{out: "FAIL", err: errors.New("exit 1")}
	h := Deps{Agent: m, WS: f.ws, Workdir: "/tmp/wd", Run: fr, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "run", `{"program":"go","args":["test"]}`, f)
	out, isErr := resolve(t)
	if !isErr || !strings.Contains(out, "FAIL") {
		t.Fatalf("failing run = %q isErr=%v, want output preserved", out, isErr)
	}
}

func TestAskHumanPostsToThread(t *testing.T) {
	f, m := newFixture(t, "ask_human")
	h := Deps{Agent: m, WS: f.ws, Bus: f.bus, Approvals: f.approvals,
		Channel: "#general"}.Handler()
	out, isErr := call(h, t, "ask_human", `{"question":"which branch should I use?"}`)
	if isErr {
		t.Fatalf("ask_human refused: %s", out)
	}
	hist := f.bus.History("#general", 0)
	if len(hist) == 0 || !strings.Contains(hist[len(hist)-1].Text, "which branch") {
		t.Fatalf("question not posted: %+v", hist)
	}
	if hist[len(hist)-1].Author != "scout" {
		t.Fatalf("author = %q, want the agent", hist[len(hist)-1].Author)
	}
}

func TestAskHumanRefusesWithoutChannel(t *testing.T) {
	f, m := newFixture(t, "ask_human")
	h := Deps{Agent: m, WS: f.ws, Bus: f.bus, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "ask_human", `{"question":"hi"}`)
	if !isErr || !strings.Contains(out, "channel") {
		t.Fatalf("no-channel ask = %q isErr=%v", out, isErr)
	}
}

func TestScopeDenyRefusesByName(t *testing.T) {
	f, m := newFixture(t, "task_create")
	h := Deps{Agent: m, WS: f.ws, Tasks: f.tasks, Approvals: f.approvals,
		Scopes: map[scopes.Scope]scopes.Effect{scopes.Write: scopes.Deny},
	}.Handler()
	out, isErr := call(h, t, "task_create", `{"slug":"x","title":"y"}`)
	if !isErr || !strings.Contains(out, "denied by capability scope") {
		t.Fatalf("deny = %q isErr=%v", out, isErr)
	}
	if len(f.approvals.List()) != 0 {
		t.Fatal("denied tool parked an approval")
	}
}

func TestScopeAutoSkipsApproval(t *testing.T) {
	f, m := newFixture(t, "write")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals,
		Scopes: map[scopes.Scope]scopes.Effect{scopes.Write: scopes.Auto},
	}.Handler()
	// No approval parked: the call returns synchronously.
	out, isErr := call(h, t, "write", `{"path":"api/auto.go","content":"x\n"}`)
	if isErr {
		t.Fatalf("auto write refused: %s", out)
	}
}
