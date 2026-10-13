package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// teamHarness: scout leads team "core" with dev and rev (a reviewer),
// all rostered, a task store and episodes.
func teamHarness(t *testing.T) (*harness, *tasks.Store) {
	t.Helper()
	h := episodeHarness(t)
	scout, err := manifest.Parse("scout", []byte(baseDoc()))
	if err != nil {
		t.Fatal(err)
	}
	rev, err := manifest.Parse("rev", []byte("schema = 2\nname = \"Rev\"\nmodel = \"m\"\nruntime = \"claude\"\nrole = \"reviewer\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Reload([]*manifest.Agent{scout, mustAgent(t, "dev", "Dev"), rev}); err != nil {
		t.Fatal(err)
	}
	o, err := org.Load(h.ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.CreateTeam("core", "scout", []string{"scout", "dev", "rev"}); err != nil {
		t.Fatal(err)
	}
	h.rt.cfg.Org = o
	st, err := tasks.Open(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	h.rt.cfg.Tasks = st
	return h, st
}

// F-071: a lead's task prompt carries the roster and the invitation to
// split the work.
func TestLeadSeesRosterAndSplitInvite(t *testing.T) {
	h, st := teamHarness(t)
	trig := mustPost(t, h, bus.Message{Channel: "#core", Author: bus.Human, Text: "@scout build billing"})
	if err := st.Create("billing", "Build billing", "scout", "core"); err != nil {
		t.Fatal(err)
	}
	if err := st.BindThread("billing", "#core", trig.ID); err != nil {
		t.Fatal(err)
	}
	prompt, _ := h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], trig, "")
	if !strings.Contains(prompt, "you lead team core: dev (member), rev (reviewer)") || !strings.Contains(prompt, `parent="billing"`) {
		t.Fatalf("lead block missing:\n%s", prompt)
	}
	// A sub-task's own prompt shows its parent and blockers instead.
	if err := st.Create("schema", "Schema", "dev", "core"); err != nil {
		t.Fatal(err)
	}
	if err := st.Create("api", "API", "dev", "core"); err != nil {
		t.Fatal(err)
	}
	_ = st.SetPlan("schema", "billing", nil, false)
	_ = st.SetPlan("api", "billing", []string{"schema"}, true)
	prompt, _ = h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], trig, "")
	if !strings.Contains(prompt, "sub-tasks 0/2 done") || !strings.Contains(prompt, "open this card's PR only after every sub-task") {
		t.Fatalf("parent block missing:\n%s", prompt)
	}
	if reasons := h.rt.childGate(trig)(workflow.SeamPR); len(reasons) != 1 || !strings.Contains(reasons[0], "api, schema") {
		t.Fatalf("parent PR gate = %v", reasons)
	}
}

// A held sub-card is handed off — bound and dispatched — when its blocker
// is done.
func TestReleaseReadyHandsOffUnblocked(t *testing.T) {
	h, st := teamHarness(t)
	for _, slug := range []string{"schema", "api"} {
		if err := st.Create(slug, strings.ToUpper(slug), "dev", "core"); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.SetPlan("api", "", []string{"schema"}, true)
	h.rt.ReleaseReady(context.Background())
	if got := h.bus.History("#core", 0); len(got) != 0 {
		t.Fatalf("a blocked card must not be handed off: %+v", got)
	}
	if err := st.SetStatus("schema", tasks.Done); err != nil {
		t.Fatal(err)
	}
	h.rt.ReleaseReady(context.Background())
	brief := hasPrefix(h.bus.History("#core", 0), "@dev task api: API — unblocked: schema done")
	if brief == nil || brief.Author != systemAuthor {
		t.Fatalf("release brief missing: %+v", h.bus.History("#core", 0))
	}
	if got, _ := st.Get("api"); got.Held || got.ThreadID != brief.ID {
		t.Fatalf("card must be released and bound to its brief: %+v", got)
	}
	// The released card's assignee actually runs (dispatch).
	deadline := time.Now().Add(5 * time.Second)
	for hasPrefix(h.bus.History("#core", 0), "reply") == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hasPrefix(h.bus.History("#core", 0), "reply") == nil {
		t.Fatal("the unblocked assignee was not dispatched")
	}
}

// An agent woken by another sees the hander's working state.
func TestHandoffBrief(t *testing.T) {
	h, _ := teamHarness(t)
	root := mustPost(t, h, bus.Message{Channel: "#core", Author: bus.Human, Text: "@dev fix split"})
	if err := h.rt.cfg.Episodes.Put(episode.Episode{
		Agent: "dev", Key: episode.Key("", "#core", root.ID), Goal: "fix Split rounding",
		Plan:   []episode.PlanStep{{Title: "write failing test", Status: "done"}, {Title: "fix rounding", Status: "doing"}},
		Verify: "fail", VerifyCmd: "go test ./...", LastSummary: "remainder still dropped",
	}); err != nil {
		t.Fatal(err)
	}
	hand := mustPost(t, h, bus.Message{Channel: "#core", Thread: root.ID, Author: "dev", Text: "@scout can you take over the rounding?"})
	prompt, _ := h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], hand, "")
	for _, want := range []string{"Handed over by @dev", "goal: fix Split rounding", "[done] write failing test", "DHI verify: fail go test ./...", "remainder still dropped"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("handoff brief missing %q:\n%s", want, prompt)
		}
	}
}

// A PR opened by dev goes to the team's reviewer first.
func TestPROpenedAsksReviewer(t *testing.T) {
	h, st := teamHarness(t)
	trig := mustPost(t, h, bus.Message{Channel: "#core", Author: bus.Human, Text: "@dev do it"})
	if err := st.Create("billing", "Billing", "dev", "core"); err != nil {
		t.Fatal(err)
	}
	_ = st.BindThread("billing", "#core", trig.ID)
	if got := h.rt.reviewerFor("dev", tasks.Task{Team: "core"}); got != "rev" {
		t.Fatalf("reviewer = %q", got)
	}
	if got := h.rt.reviewerFor("rev", tasks.Task{Team: "core"}); got != "" {
		t.Fatalf("a reviewer never reviews itself: %q", got)
	}
	h.rt.prOpened(context.Background(), "dev", trig)("billing", []string{"api: PR #7"})
	ask := hasPrefix(h.bus.History("#core", trig.ID), "@rev please review the PR for task billing (api: PR #7)")
	if ask == nil || ask.Author != "dev" {
		t.Fatalf("review request missing: %+v", h.bus.History("#core", trig.ID))
	}
}
