package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
)

func dmTrigger(t *testing.T, h *harness, text string) bus.Message {
	t.Helper()
	return mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: text})
}

func TestReadOnlyTurnAskDeniesWriteToolsAndReplies(t *testing.T) {
	h := episodeHarness(t)
	trig := dmTrigger(t, h, "/ask where is main")
	if err := h.rt.ReadOnlyTurn(context.Background(), "scout", trig, "ask", "where is main"); err != nil {
		t.Fatal(err)
	}
	args := spawnArgs(t, h, 1)
	for _, want := range []string{"--disallowedTools", "Edit", "Bash", "MODE: ask", "where is main"} {
		if !strings.Contains(args, want) {
			t.Errorf("argv lacks %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--resume") {
		t.Error("a read-only turn never resumes")
	}
	if hasPrefix(h.bus.History("dm:scout", 0), "reply 1") == nil {
		t.Fatal("the answer must be posted as the agent's reply")
	}
}

func TestReadOnlyTurnPlanWithoutPlanUpdateSaysSo(t *testing.T) {
	h := episodeHarness(t)
	trig := dmTrigger(t, h, "/plan add a flag")
	if err := h.rt.ReadOnlyTurn(context.Background(), "scout", trig, "plan", "add a flag"); err == nil {
		t.Fatal("a plan turn that publishes nothing must report it")
	}
	if !strings.Contains(spawnArgs(t, h, 1), "MODE: plan") {
		t.Error("the plan prompt must carry the plan mode")
	}
	if hasPrefix(h.bus.History("dm:scout", 0), "I produced no plan") == nil {
		t.Fatal("the missing plan must be named in the thread")
	}
	if len(h.approvals.List()) != 0 {
		t.Fatal("no plan, no approval card")
	}
}

func TestReadOnlyTurnRefusesACLIWithoutAReadOnlyMode(t *testing.T) {
	h := episodeHarness(t)
	cur, ok := h.rt.cfg.CLIs.Get("cursor-agent")
	if !ok {
		t.Fatal("no cursor-agent adapter")
	}
	h.rt.agents["scout"].cli = cur
	trig := dmTrigger(t, h, "/plan x")
	err := h.rt.ReadOnlyTurn(context.Background(), "scout", trig, "plan", "x")
	if err == nil {
		t.Fatal("must refuse")
	}
	if m := hasPrefix(h.bus.History("dm:scout", 0), "/plan needs a read-only run and cursor-agent has none"); m == nil {
		t.Fatalf("refusal must name the CLI: %v", h.bus.History("dm:scout", 0))
	}
	if _, serr := os.Stat(filepath.Join(h.dumpDir, "spawn-1")); serr == nil {
		t.Fatal("nothing may be started")
	}
}

func waitCard(t *testing.T, h *harness) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if len(h.approvals.List()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no approval card appeared")
}

var testPlan = []dhitools.PlanStep{{Title: "Add the flag", Status: "todo"}, {Title: "Test it", Status: "todo"}}

func TestPlanApprovalRunsTheWriteTurnWithThePlan(t *testing.T) {
	h := episodeHarness(t)
	trig := dmTrigger(t, h, "/plan add a flag")
	done := make(chan error, 1)
	go func() { done <- h.rt.gatePlan(context.Background(), "scout", trig, "I will add it.", testPlan) }()
	waitCard(t, h)
	ap := h.approvals.List()[0]
	if !ap.IsPlan() || !strings.Contains(ap.Describe(), "2 steps") {
		t.Fatalf("card = %+v", ap)
	}
	h.approvals.Decide(ap, "always") // a plan is never remembered
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.rt.bg.Wait()
	if h.approvals.Granted("scout", ap.Scope) {
		t.Fatal("approving a plan must not leave a standing grant")
	}
	args := spawnArgs(t, h, 1)
	if !strings.Contains(args, "Plan approved") || !strings.Contains(args, "1. Add the flag") {
		t.Fatalf("the write turn must carry the plan:\n%s", args)
	}
	if strings.Contains(args, "MultiEdit") || strings.Contains(args, "MODE: plan") {
		t.Fatalf("the approved turn is a normal write turn:\n%s", args)
	}
}

func TestPlanRejectionWritesNothingAndKeepsTheDraft(t *testing.T) {
	h := episodeHarness(t)
	trig := dmTrigger(t, h, "/plan add a flag")
	key := episode.Key("", "dm:scout", trig.ID)
	if err := h.rt.planSeam("scout", key)(testPlan); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.rt.gatePlan(context.Background(), "scout", trig, "", testPlan) }()
	waitCard(t, h)
	h.approvals.Decide(h.approvals.List()[0], "no")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.rt.bg.Wait()
	if _, err := os.Stat(filepath.Join(h.dumpDir, "spawn-1")); err == nil {
		t.Fatal("a rejected plan must start no turn")
	}
	if hasPrefix(h.bus.History("dm:scout", 0), "plan rejected") == nil {
		t.Fatal("the rejection must be named")
	}
	ep, ok, _ := h.rt.cfg.Episodes.Get("scout", key)
	if !ok || len(ep.Plan) != 2 {
		t.Fatalf("the plan stays a draft on the episode: %+v", ep)
	}
}

func TestReadOnlyTurnScopesDenyEverythingButRead(t *testing.T) {
	h := episodeHarness(t)
	e := h.rt.agents["scout"]
	set := h.rt.turnScopes(e, turnTools{readOnly: true})
	if set.EffectFor(scopes.Read) != scopes.Auto {
		t.Error("read must stay free")
	}
	for _, sc := range []scopes.Scope{scopes.Write, scopes.Exec, scopes.Git, scopes.Push, scopes.Network, scopes.Admin} {
		if set.EffectFor(sc) != scopes.Deny {
			t.Errorf("%s must be denied in a read-only turn", sc)
		}
	}
	if h.rt.turnScopes(e, turnTools{}).EffectFor(scopes.Write) == scopes.Deny {
		t.Error("a normal turn keeps the agent's scopes")
	}
}
