package runtime

import (
	"context"
	"strings"
	"sync"
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
	// Hand-offs start turns in the background; drain them before the
	// temp workspace is removed (cleanups run last-registered first).
	t.Cleanup(h.rt.WaitBackground)
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
	if !strings.Contains(prompt, "sub-tasks 0/2 done") || !strings.Contains(prompt, "this card tracks its sub-tasks") {
		t.Fatalf("parent block missing:\n%s", prompt)
	}
	if reasons := h.rt.childGate(trig)(workflow.SeamPR); len(reasons) != 1 || !strings.Contains(reasons[0], "tracks sub-tasks") {
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
	if ask != nil && !strings.Contains(ask.Text, "Reply to @dev") {
		t.Fatalf("the reviewer must be told to answer the author: %q", ask.Text)
	}
	if ask == nil || ask.Author != "dev" {
		t.Fatalf("review request missing: %+v", h.bus.History("#core", trig.ID))
	}
}

// The watcher and a turn's end may release at once: one brief, not two.
func TestReleaseReadyConcurrentOnce(t *testing.T) {
	h, st := teamHarness(t)
	for _, slug := range []string{"schema", "api"} {
		if err := st.Create(slug, strings.ToUpper(slug), "dev", "core"); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.SetPlan("api", "", []string{"schema"}, true)
	_ = st.SetStatus("schema", tasks.Done)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); h.rt.ReleaseReady(context.Background()) }()
	}
	wg.Wait()
	n := 0
	for _, m := range h.bus.History("#core", 0) {
		if strings.HasPrefix(m.Text, "@dev task api") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("briefs = %d, want exactly 1", n)
	}
}

// A reviewer agent's findings are a learning signal for the author; its
// sign-off is not.
func TestReviewerFindingsAreCorrections(t *testing.T) {
	h, _ := teamHarness(t)
	root := mustPost(t, h, bus.Message{Channel: "#core", Author: bus.Human, Text: "@dev do it"})
	mustPost(t, h, bus.Message{Channel: "#core", Thread: root.ID, Author: "dev", Text: "done, PR #7", Kind: bus.KindReply})
	finding := mustPost(t, h, bus.Message{Channel: "#core", Thread: root.ID, Author: "rev", Text: "@dev money.go:12 drops the remainder"})
	tl := &turnLog{}
	h.rt.noteCorrection("dev", finding, tl)
	if len(tl.signals) != 1 || tl.correction != "money.go:12 drops the remainder" {
		t.Fatalf("finding not a correction: %+v", tl)
	}
	ok := mustPost(t, h, bus.Message{Channel: "#core", Thread: root.ID, Author: "rev", Text: "@dev LGTM, ready"})
	tl = &turnLog{}
	h.rt.noteCorrection("dev", ok, tl)
	if len(tl.signals) != 0 {
		t.Fatal("a sign-off is not a correction")
	}
}

// A tracking card (with sub-tasks) is never provisioned.
func TestTrackingCardNotProvisioned(t *testing.T) {
	h, st := teamHarness(t)
	for _, slug := range []string{"epic", "part"} {
		if err := st.Create(slug, slug, "scout", "core"); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.SetPlan("part", "epic", nil, false)
	called := false
	h.rt.cfg.Provision = func(string, string) error { called = true; return nil }
	epic, _ := st.Get("epic")
	h.rt.provisionWorktree("scout", bus.Message{Channel: "#core"}, epic)
	if called {
		t.Fatal("a tracking card must not get a worktree")
	}
}

// F-073: an agent moving a card to another agent hands it off, naming
// the worktree; the human's own reassignments are the board's to hand off.
func TestAgentReassignHandsOff(t *testing.T) {
	h, st := teamHarness(t)
	if err := st.Create("csv", "CSV export", "scout", ""); err != nil {
		t.Fatal(err)
	}
	handed := map[string]time.Time{}
	if err := st.AssignAs("csv", "dev", bus.Human); err != nil {
		t.Fatal(err)
	}
	h.rt.agentReassigned(context.Background(), "csv", handed)
	if n := len(h.bus.History("dm:dev", 0)); n != 0 {
		t.Fatalf("human reassignment handed off by the runtime too (%d posts)", n)
	}
	if err := st.AssignAs("csv", "rev", "dev"); err != nil {
		t.Fatal(err)
	}
	h.rt.agentReassigned(context.Background(), "csv", handed)
	h.rt.agentReassigned(context.Background(), "csv", handed) // a repeat change event is not a second hand-off
	posts := h.bus.History("dm:rev", 0)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "reassigned by dev") || posts[0].Author != "dev" {
		t.Fatalf("hand-off posts = %+v", posts)
	}
	if tk, _ := st.Get("csv"); tk.ThreadChannel != "dm:rev" {
		t.Fatalf("card not bound to the brief: %+v", tk.ThreadChannel)
	}
}

// A lead assigning a held sub-card, or a card's first assignment, is
// not a hand-off: ReleaseReady hands the card over when its blockers
// finish.
func TestAgentAssignOfHeldOrNewCardIsNotHandedOff(t *testing.T) {
	h, st := teamHarness(t)
	handed := map[string]time.Time{}
	if err := st.Create("api", "API", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignAs("api", "dev", "scout"); err != nil { // first assignment
		t.Fatal(err)
	}
	h.rt.agentReassigned(context.Background(), "api", handed)
	if err := st.Create("ui", "UI", "dev", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPlan("ui", "", []string{"api"}, true); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignAs("ui", "rev", "scout"); err != nil { // held: waits on api
		t.Fatal(err)
	}
	h.rt.agentReassigned(context.Background(), "ui", handed)
	for _, ch := range []string{"dm:dev", "dm:rev"} {
		if n := len(h.bus.History(ch, 0)); n != 0 {
			t.Fatalf("%s got %d hand-off posts", ch, n)
		}
	}
}
