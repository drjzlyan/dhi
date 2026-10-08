package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// dispatchRT builds a runtime with rostered agents (no CLI behind them:
// these tests only exercise routing, never a Turn).
func dispatchRT(t *testing.T, agents ...string) (*Runtime, *org.Org, *bus.Bus) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	o, err := org.Load(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{cfg: Config{WS: ws, Bus: b, Org: o}, agents: map[string]*entry{}}
	for _, id := range agents {
		rt.agents[id] = &entry{}
	}
	return rt, o, b
}

func TestTargetsMentionsAndLead(t *testing.T) {
	rt, o, _ := dispatchRT(t, "ana", "bo", "cy")
	if err := o.CreateTeam("web", "ana", []string{"ana", "bo"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		msg  bus.Message
		want string
	}{
		{"human bare post goes to lead", bus.Message{Channel: "#web", Author: "you", Text: "ship login"}, "ana"},
		{"mention beats lead", bus.Message{Channel: "#web", Author: "you", Text: "@bo ship login"}, "bo"},
		{"agent bare post does not route to lead", bus.Message{Channel: "#web", Author: "bo", Text: "done"}, ""},
		{"agent mention wakes addressee", bus.Message{Channel: "#web", Author: "ana", Text: "@bo take this"}, "bo"},
		{"self mention ignored", bus.Message{Channel: "#web", Author: "ana", Text: "@ana"}, ""},
		{"unknown agent ignored", bus.Message{Channel: "#web", Author: "ana", Text: "@ghost"}, ""},
		{"non-team channel bare post wakes nobody", bus.Message{Channel: "#general", Author: "you", Text: "hi"}, ""},
	}
	for _, c := range cases {
		got := strings.Join(rt.targets(c.msg), ",")
		if got != c.want {
			t.Errorf("%s: targets = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHopBudgetStopsAgentLoops(t *testing.T) {
	rt, _, b := dispatchRT(t, "ana", "bo")
	for i := 0; i < maxAgentHops; i++ {
		if !rt.takeHop("#general") {
			t.Fatalf("hop %d refused before budget", i)
		}
	}
	// An exhausted channel posts the notice and dispatches nothing
	// (no Turn goroutine is started — entries have no CLI behind them).
	rt.Handle(context.Background(), bus.Message{Channel: "#general", Author: "ana", Text: "@bo your turn"})
	hist := b.History("#general", 0)
	if len(hist) != 1 || hist[0].Author != systemAuthor || !strings.Contains(hist[0].Text, "hand-off limit") {
		t.Fatalf("want one hand-off limit notice, got %+v", hist)
	}
	// A human message resets the budget.
	rt.resetHops("#general")
	if !rt.takeHop("#general") {
		t.Fatal("budget should reset after a human message")
	}
	// Budgets are per channel.
	if !rt.takeHop("#other") {
		t.Fatal("other channel should have its own budget")
	}
}

func TestTeamWithoutLeadNamesTheFix(t *testing.T) {
	rt, o, b := dispatchRT(t, "ana")
	if err := o.CreateTeam("ops", "", []string{"ana"}); err != nil {
		t.Fatal(err)
	}
	rt.Handle(context.Background(), bus.Message{Channel: "#ops", Author: "you", Text: "deploy"})
	hist := b.History("#ops", 0)
	if len(hist) != 1 || !strings.Contains(hist[0].Text, "has no lead") {
		t.Fatalf("want no-lead notice, got %+v", hist)
	}
}

func TestHumanLeadStaysQuiet(t *testing.T) {
	rt, o, b := dispatchRT(t, "ana")
	if err := o.CreateTeam("ops", org.Human, []string{"ana"}); err != nil {
		t.Fatal(err)
	}
	rt.Handle(context.Background(), bus.Message{Channel: "#ops", Author: "you", Text: "fyi"})
	if hist := b.History("#ops", 0); len(hist) != 0 {
		t.Fatalf("human lead must not trigger a notice, got %+v", hist)
	}
}

func TestActiveCountSumsInFlightTurns(t *testing.T) {
	rt, _, _ := dispatchRT(t, "ana")
	if rt.ActiveCount() != 0 {
		t.Fatal("idle runtime reports activity")
	}
	rt.workStart("#a#0")
	rt.workStart("#a#0")
	rt.workStart("#b#7")
	if got := rt.ActiveCount(); got != 3 {
		t.Fatalf("ActiveCount = %d, want 3", got)
	}
	rt.workDone("#a#0")
	rt.workDone("#b#7")
	rt.workDone("#b#7") // extra done never goes negative
	if got := rt.ActiveCount(); got != 1 {
		t.Fatalf("ActiveCount = %d, want 1", got)
	}
}

// TestSessionChannelsAreRoutedByTheFloorProtocolNotTheRuntime guards the
// M18/M21 interaction: an agent @mention inside an ideation session must
// not be dispatched by the runtime (the ideator's floor protocol hands the
// floor on, with moderator accounting and its own turn cap). Probe: with
// the runtime's hop budget exhausted, a dispatchable agent message posts a
// "hand-off limit" notice; an ignored one posts nothing.
func TestSessionChannelsAreRoutedByTheFloorProtocolNotTheRuntime(t *testing.T) {
	rt, _, b := dispatchRT(t, "ana", "bo")
	session := ideation.ChannelFor("roadmap")
	for _, ch := range []string{session, "#web"} {
		for i := 0; i < maxAgentHops; i++ {
			rt.takeHop(ch)
		}
	}

	rt.Handle(context.Background(), bus.Message{Channel: session, Author: "ana", Text: "@bo your thoughts?"})
	if hist := b.History(session, 0); len(hist) != 0 {
		t.Fatalf("runtime must stay out of a session channel, got %+v", hist)
	}
	if rt.takeHop(session) {
		t.Fatal("a session message must not touch the runtime hop budget either")
	}

	// Control: the same message in an ordinary channel IS dispatchable
	// (the exhausted budget turns it into the visible notice).
	rt.Handle(context.Background(), bus.Message{Channel: "#web", Author: "ana", Text: "@bo your thoughts?"})
	hist := b.History("#web", 0)
	if len(hist) != 1 || !strings.Contains(hist[0].Text, "hand-off limit") {
		t.Fatalf("control case should have been dispatched, got %+v", hist)
	}

	// A HUMAN post in a session still goes through (the ideator calls
	// Handle for the moderator's grants and invites).
	if got := rt.targets(bus.Message{Channel: session, Author: "you", Text: "@bo please start"}); len(got) != 1 || got[0] != "bo" {
		t.Fatalf("human mentions in a session must still route, got %v", got)
	}
}

// twoAgentRuntime builds a REAL runtime (real Handle/Turn/cliTurn) with
// agents ana and bo on a stub CLI that appends one line to a spawn
// counter per run and always answers "…@bo please continue".
func twoAgentRuntime(t *testing.T) (*Runtime, *bus.Bus, func() int) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(t.TempDir(), "spawns")
	stub := "#!/bin/sh\necho x >> " + counter + "\n" +
		`printf '%s\n' '{"type":"system","subtype":"init"}'` + "\n" +
		`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"on it, @bo please continue","usage":{"input_tokens":1,"output_tokens":1}}'` + "\n"
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := clirun.NewRegistry(func(name string) (string, error) { return filepath.Join(binDir, name), nil })
	var agents []*manifest.Agent
	for _, id := range []string{"ana", "bo"} {
		m, perr := manifest.Parse(id, []byte("schema = 1\nname = \""+id+"\"\nmodel = \"m\"\nruntime = \"claude\"\n"))
		if perr != nil {
			t.Fatal(perr)
		}
		agents = append(agents, m)
	}
	rt, err := New(Config{
		WS: ws, Bus: b, Approvals: tools.NewApprovals(), Sandbox: &recordingSandbox{}, CLIs: reg,
		CLIEnv: []string{"PATH=" + binDir + ":/usr/bin:/bin"},
	}, agents)
	if err != nil {
		t.Fatal(err)
	}
	spawns := func() int {
		data, _ := os.ReadFile(counter)
		return strings.Count(string(data), "x")
	}
	return rt, b, spawns
}

func waitSpawns(t *testing.T, spawns func() int, want int) {
	t.Helper()
	deadline := time.Now().Add(asyncTimeout)
	for time.Now().Before(deadline) {
		if spawns() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d spawn(s); have %d", want, spawns())
}

// A real chain in an ordinary channel: ana's reply @mentions bo, so the
// runtime wakes bo — exactly two CLI spawns.
func TestAgentMentionWakesTheAddresseeThroughTheRealRuntime(t *testing.T) {
	rt, b, spawns := twoAgentRuntime(t)
	posted, _ := b.Post(bus.Message{Channel: "#web", Author: "you", Text: "@ana go"})
	rt.Handle(context.Background(), posted)
	waitSpawns(t, spawns, 2)
	time.Sleep(400 * time.Millisecond) // a runaway chain would show up as >2
	if got := spawns(); got != 2 {
		t.Fatalf("spawns = %d, want exactly 2 (ana then bo)", got)
	}
}

// The same chain inside an ideation session must NOT be runtime-routed:
// the ideator's floor protocol owns hand-offs there, so ana's @bo wakes
// no one from the runtime side — exactly one spawn, no double dispatch.
func TestSessionChannelAgentMentionIsNotDoubleDispatched(t *testing.T) {
	rt, b, spawns := twoAgentRuntime(t)
	ch := ideation.ChannelFor("roadmap")
	sub, cancel := b.Subscribe(ch)
	defer cancel()
	posted, _ := b.Post(bus.Message{Channel: ch, Author: "you", Text: "@ana go"})
	rt.Handle(context.Background(), posted)
	deadline := time.After(asyncTimeout) // the subscription also carries the human post first
	for done := false; !done; {
		select {
		case m := <-sub:
			done = m.Author == "ana" && strings.Contains(m.Text, "@bo")
		case <-deadline:
			t.Fatal("ana never replied")
		}
	}
	time.Sleep(600 * time.Millisecond)
	if got := spawns(); got != 1 {
		t.Fatalf("spawns = %d, want exactly 1: the runtime must not dispatch @bo inside a session", got)
	}
}
