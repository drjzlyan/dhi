package reviewer

import (
	"context"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/provider"
	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// fakeCrew records Handle dispatches without running a real runtime.
type fakeCrew struct {
	handled []bus.Message
	ids     []string
	reply   func(bus.Message) // optional synchronous reply simulation
}

func (f *fakeCrew) Handle(_ context.Context, msg bus.Message) {
	f.handled = append(f.handled, msg)
	if f.reply != nil {
		f.reply(msg)
	}
}
func (f *fakeCrew) AgentIDs() []string { return f.ids }

// newAgentSurface wires the reviewer with a real bus + fake crew over
// the standard fixture workspace.
func newAgentSurface(t *testing.T) (*Model, *review.Store, *bus.Bus, *fakeCrew) {
	t.Helper()
	m, ws, st, _ := newSurface(t)
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeCrew{ids: []string{"rev"}}
	m.bus = b
	m.crew = fc
	return m, st, b, fc
}

func drainBusEvents(t *testing.T, m *Model, wantAuthor string, max int) bool {
	t.Helper()
	for i := 0; i < max; i++ {
		msg := pumpCmd(t, m.listen())
		if ev, ok := msg.(revEvent); ok && ev.kind == evBus && ev.msg.Author == wantAuthor {
			_ = m.Update(ev)
			return true
		} else if ok {
			_ = m.Update(ev)
		}
	}
	return false
}

func TestInviteDispatchesAndMirrorsReply(t *testing.T) {
	m, st, b, fc := newAgentSurface(t)
	r := startBranchReview(t, m, st)

	// comment mentioning the agent on a diff line
	commentOnFirstLine(t, m, "@rev why this locking approach?")

	// invite dispatched to the crew with our mention text
	if len(fc.handled) != 1 {
		t.Fatalf("crew handled = %d", len(fc.handled))
	}
	if !strings.Contains(fc.handled[0].Text, "@rev") ||
		fc.handled[0].Channel != r.Channel {
		t.Fatalf("dispatched = %+v", fc.handled[0])
	}

	// thread now carries the bus root for reply correlation
	cur, _ := st.Get(r.ID)
	if len(cur.Threads) != 1 || cur.Threads[0].BusThread == 0 {
		t.Fatalf("threads = %+v", cur.Threads)
	}

	// agent replies on the bus in-thread; surface mirrors it as an
	// immutable comment authored by the agent.
	_, err := b.Post(bus.Message{Channel: r.Channel,
		Thread: cur.Threads[0].BusThread,
		Author: "rev", Text: "double-checked locking is fine here"})
	if err != nil {
		t.Fatal(err)
	}
	if !drainBusEvents(t, m, "rev", 8) {
		t.Fatal("agent reply never mirrored")
	}
	got, _ := st.Get(r.ID)
	c := got.Threads[0].Comments
	if len(c) != 2 || c[1].Author != "rev" || c[1].Pending {
		t.Fatalf("mirrored comments = %+v", c)
	}
}

func TestUnmatchedAgentReplyBecomesFileLevelThread(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	m.subscribeBus(r.ID)

	if _, err := b.Post(bus.Message{Channel: r.Channel,
		Author: "rev", Text: "overall LGTM"}); err != nil {
		t.Fatal(err)
	}
	if !drainBusEvents(t, m, "rev", 8) {
		t.Fatal("no mirror event")
	}
	got, _ := st.Get(r.ID)
	if len(got.Threads) != 1 || got.Threads[0].File != "(review)" ||
		got.Threads[0].Comments[0].Text != "overall LGTM" {
		t.Fatalf("threads = %+v", got.Threads)
	}
}

func TestCompleteAgentReviewMode(t *testing.T) {
	m, st, _, fc := newAgentSurface(t)
	r := startBranchReview(t, m, st)

	m.HandleKey("A") // FILES section: agent review modal
	if m.form.kind != fAgentReview {
		t.Fatalf("kind = %v", m.form.kind)
	}
	if m.form.fields[0].text() != "rev" {
		t.Errorf("default agent = %q", m.form.fields[0].text())
	}
	m.submitForm()

	// completion event arrives after the synchronous fake dispatch
	msg2 := pumpCmd(t, m.listen())
	_ = m.Update(msg2)
	if len(fc.handled) != 1 {
		t.Fatal("complete-review request never dispatched")
	}
	msg := fc.handled[0]
	if !strings.Contains(msg.Text, "@rev") || !strings.Contains(msg.Text, "```diff") {
		t.Fatalf("prompt = %q", msg.Text)
	}
	if m.busy || m.opErr != "" {
		t.Fatalf("busy=%v err=%q", m.busy, m.opErr)
	}
	_ = r
}

func TestCompleteAgentReviewUnknownAgentRejected(t *testing.T) {
	m, st, _, fc := newAgentSurface(t)
	startBranchReview(t, m, st)
	m.HandleKey("A")
	m.form.fields[0].runes = []rune("ghost")
	m.submitForm()
	if m.form.err == "" {
		t.Fatal("unknown agent accepted")
	}
	if len(fc.handled) != 0 {
		t.Fatal("dispatched despite unknown agent")
	}
}

// TestMockProviderEndToEnd runs the F-005 acceptance flow against the
// real runtime + scripted MockProvider: invite → turn → reply lands
// in-thread as an immutable comment authored by the agent.
func TestMockProviderEndToEnd(t *testing.T) {
	m, ws, st, _ := newSurface(t)
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	mf, err := manifest.Parse("rev", []byte("schema = 1\nname = \"Rev\"\nmodel = \"mock-1\"\nsystem = \"You review code.\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(runtime.Config{
		WS:        ws,
		Bus:       b,
		Approvals: tools.NewApprovals(),
		Provider:  provider.NewMock(provider.ScriptText("looked at it: the locking is fine")),
		Sandbox:   sandbox.Noop{},
	}, []*manifest.Agent{mf})
	if err != nil {
		t.Fatal(err)
	}
	m.bus = b
	m.crew = rt

	r := startBranchReview(t, m, st)
	commentOnFirstLine(t, m, "@rev why this locking approach?")

	if !drainBusEvents(t, m, "rev", 16) {
		t.Fatal("agent reply never mirrored")
	}
	got, _ := st.Get(r.ID)
	c := got.Threads[0].Comments
	if len(c) != 2 || c[1].Author != "rev" ||
		!strings.Contains(c[1].Text, "locking is fine") || c[1].Pending {
		t.Fatalf("thread comments = %+v", c)
	}
}
