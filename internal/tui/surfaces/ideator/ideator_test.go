package ideator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// fakeCrew records dispatched turns.
type fakeCrew struct{ handled []bus.Message }

func (f *fakeCrew) Handle(_ context.Context, msg bus.Message) { f.handled = append(f.handled, msg) }
func (f *fakeCrew) AgentIDs() []string                        { return []string{"scout", "mason"} }

// newSurface builds a fully-wired ideator over a temp workspace: member
// "api" is an ordinary directory, the session store is real, the bus is
// real (local JSONL), and the crew is a scripted fake. No network, no
// provider.
func newSurface(t *testing.T) (*Model, *workspace.Workspace, *ideation.Store, *fakeCrew, *bus.Bus) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema = 1\n\n[members.api]\npath = \"api\"\n"
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := ideation.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	crew := &fakeCrew{}
	m := New("0.1.0", ws, Deps{Store: st, Bus: b, Crew: crew})
	m.Resize(100, 30)
	return m, ws, st, crew, b
}

func pumpCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	type boxed struct {
		msg tea.Msg
	}
	ch := make(chan boxed, 1)
	go func() { ch <- boxed{cmd()} }()
	select {
	case b := <-ch:
		return b.msg
	case <-time.After(3 * time.Second):
		t.Fatal("async operation timed out")
	}
	return nil
}

// createSession drives the modal: name / topic / agents, enter, pump.
func createSession(t *testing.T, m *Model, name, topic, agents string) ideation.Session {
	t.Helper()
	m.HandleKey("n")
	if m.form.kind != fNewSession {
		t.Fatalf("expected fNewSession modal, got %v", m.form.kind)
	}
	m.form.fields[0].runes = []rune(name)
	m.form.fields[1].runes = []rune(topic)
	m.form.fields[2].runes = []rune(agents)
	m.HandleKey("enter")
	msg := pumpCmd(t, m.listen())
	ev, ok := msg.(ideEvent)
	if !ok || ev.kind != evCreated || ev.err != "" {
		t.Fatalf("create event = %+v", msg)
	}
	m.Update(msg)
	sess, ok := m.store.Get(ev.id)
	if !ok {
		t.Fatalf("session %q not persisted", ev.id)
	}
	return sess
}

// writeArtifact drops a file into the session folder.
func writeArtifact(t *testing.T, m *Model, id, rel, content string) {
	t.Helper()
	abs := m.store.ArtifactPath(id, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsNavAndOpen(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	a, _ := st.Create("Alternatives", "pick one", []string{"scout"})
	_, _ = st.Create("Docs plan", "", nil)

	if !m.HandleKey("j") || m.cursors[secSessions] != 1 {
		t.Fatalf("j did not move cursor: %d", m.cursors[secSessions])
	}
	if !m.HandleKey("k") || m.cursors[secSessions] != 0 {
		t.Fatal("k did not move cursor back")
	}
	m.HandleKey("enter")
	if m.sec != secCanvas || m.openID != a.ID {
		t.Fatalf("open: sec=%v openID=%q", m.sec, m.openID)
	}
	m.HandleKey("esc")
	if m.sec != secSessions {
		t.Fatal("esc did not return to SESSIONS")
	}
}

func TestCreateSessionFlow(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	sess := createSession(t, m, "Payment retries", "idempotency", "scout, mason")
	if sess.ID != "payment-retries" || sess.Channel != "#ideation-payment-retries" {
		t.Fatalf("session = %+v", sess)
	}
	if sess.Mode != ideation.ModeGroup {
		t.Fatalf("default mode = %q", sess.Mode)
	}
	if m.sec != secCanvas || m.openID != sess.ID {
		t.Fatalf("post-create state: sec=%v openID=%q", m.sec, m.openID)
	}
	// empty name is refused inside the modal
	m.HandleKey("esc")
	m.HandleKey("n")
	m.form.fields[0].runes = nil
	m.HandleKey("enter")
	if m.form.err == "" {
		t.Fatal("empty name accepted")
	}
	m.HandleKey("esc")
}

func TestCreateOneOnOneAndBreakout(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	// 1:1 via the create form's mode toggle (←/→ cycles).
	m.HandleKey("n")
	m.form.fields[0].runes = []rune("Pair")
	m.form.fields[1].runes = []rune("sync")
	m.form.fields[2].runes = []rune("scout")
	for i := 0; i < 3; i++ {
		m.HandleKey("tab") // name → topic → agents → mode
	}
	m.HandleKey("right") // group -> 1:1
	if m.form.fields[3].toggleValue() != string(ideation.ModeOneOnOne) {
		t.Fatalf("toggle = %q", m.form.fields[3].toggleValue())
	}
	m.HandleKey("enter")
	msg := pumpCmd(t, m.listen())
	ev := msg.(ideEvent)
	if ev.err != "" {
		t.Fatalf("1:1 create: %s", ev.err)
	}
	m.Update(msg)
	one, _ := st.Get(ev.id)
	if one.Mode != ideation.ModeOneOnOne {
		t.Fatalf("mode = %q", one.Mode)
	}

	// Breakout under a parent.
	parent, _ := st.Create("Parent", "", []string{"scout"})
	m.open(parent.ID)
	m.sec = secSessions
	// select the parent row (proposals first, so find its index).
	idx := -1
	for i, row := range m.sessionRows() {
		if row.sess != nil && row.sess.ID == parent.ID {
			idx = i
		}
	}
	m.cursors[secSessions] = idx
	m.HandleKey("b")
	if m.form.kind != fNewBreakout {
		t.Fatalf("b did not open breakout form: %v", m.form.kind)
	}
	m.form.fields[0].runes = []rune("Deep dive")
	m.HandleKey("enter")
	msg = pumpCmd(t, m.listen())
	ev = msg.(ideEvent)
	if ev.err != "" {
		t.Fatalf("breakout create: %s", ev.err)
	}
	m.Update(msg)
	child, _ := st.Get(ev.id)
	if !child.IsBreakout() || child.Parent != parent.ID {
		t.Fatalf("breakout = %+v", child)
	}
	// The child nests under its parent in the row list.
	nested := false
	for i, row := range m.sessionRows() {
		if row.sess != nil && row.sess.ID == child.ID && row.depth == 1 {
			nested = true
			_ = i
		}
	}
	if !nested {
		t.Fatalf("breakout did not nest: %+v", m.sessionRows())
	}
}

func TestArtifactsLifecycle(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess, _ := st.Create("Ideas", "", []string{"scout"})
	writeArtifact(t, m, sess.ID, "design.md", "# Design\n\nContent here.\n")
	writeArtifact(t, m, sess.ID, "notes.md", "plain text\n")

	m.open(sess.ID)
	m.sec = secCanvas
	if got := m.artifacts(); len(got) != 2 {
		t.Fatalf("artifacts = %+v", got)
	}

	if err := m.store.ClaimAuthor(sess.ID, "design.md", "scout"); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("v")
	a, _ := st.Artifact(sess.ID, "design.md")
	if a.Status != ideation.StatusReviewed {
		t.Fatalf("reviewed = %+v", a)
	}
	if !m.HandleKey("j") {
		t.Fatal("j")
	}
	m.HandleKey("a")
	a2, _ := st.Artifact(sess.ID, "notes.md")
	if a2.Status != ideation.StatusApproved {
		t.Fatalf("approved = %+v", a2)
	}
	m.HandleKey("r")
	m.form.fields[0].runes = []rune("too late")
	m.HandleKey("enter")
	if m.form.kind != fReject || m.form.err == "" {
		t.Fatalf("terminal approve not enforced: %+v", m.form)
	}
	m.HandleKey("esc")

	writeArtifact(t, m, sess.ID, "design.md", "# Design v2\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	a3, _ := st.Artifact(sess.ID, "design.md")
	if a3.Status != ideation.StatusDraft || a3.Author != "scout" {
		t.Fatalf("rewrite flip = %+v", a3)
	}
}

func TestRejectRoutesToAuthor(t *testing.T) {
	m, _, st, crew, b := newSurface(t)
	sess, _ := st.Create("Ideas", "", []string{"scout"})
	writeArtifact(t, m, sess.ID, "design.md", "# v1\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAuthor(sess.ID, "design.md", "scout"); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secCanvas

	m.HandleKey("r")
	if m.form.kind != fReject {
		t.Fatalf("expected fReject, got %v", m.form.kind)
	}
	m.HandleKey("enter")
	if m.form.err == "" {
		t.Fatal("empty notes accepted")
	}
	m.form.fields[0].runes = []rune("add a failure-mode table")
	m.HandleKey("enter")
	if m.form.kind != fNone {
		t.Fatalf("reject modal stuck: %+v", m.form)
	}
	a, _ := st.Artifact(sess.ID, "design.md")
	if a.Status != ideation.StatusRejected || a.Notes != "add a failure-mode table" {
		t.Fatalf("rejected artifact = %+v", a)
	}

	hist := b.History(sess.Channel, 0)
	if len(hist) != 1 {
		t.Fatalf("channel history = %+v", hist)
	}
	if !strings.Contains(hist[0].Text, "@scout") || !strings.Contains(hist[0].Text, "please revise") {
		t.Fatalf("revision text = %q", hist[0].Text)
	}
	if len(crew.handled) != 1 || crew.handled[0].ID != hist[0].ID {
		t.Fatalf("crew dispatch = %+v", crew.handled)
	}
}

func TestMirrorBusClaimsAuthor(t *testing.T) {
	m, _, st, _, b := newSurface(t)
	sess, _ := st.Create("Ideas", "", []string{"scout"})
	writeArtifact(t, m, sess.ID, "design.md", "# v1\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.subscribeBus(sess.ID)

	posted, err := b.Post(bus.Message{
		Channel: sess.Channel, Author: "scout",
		Text: "wrote .dhi/sessions/" + sess.ID + "/design.md for review",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-m.events:
		if ev.kind != evBus {
			t.Fatalf("unexpected event %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no bus event")
	}
	m.mirrorBus(posted)

	a, _ := st.Artifact(sess.ID, "design.md")
	if a.Author != "scout" {
		t.Fatalf("author not claimed: %+v", a)
	}
	other, _ := b.Post(bus.Message{Channel: "#general", Author: "scout", Text: "hi"})
	m.mirrorBus(other)
}

func TestCanvasMarkdownMermaidRaw(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess, _ := st.Create("Docs", "", nil)
	writeArtifact(t, m, sess.ID, "readme.md", "# Title\n\nSome **bold** text.\n")
	writeArtifact(t, m, sess.ID, "flow.mmd", "graph TD\n  A[Start] --> B[Done]\n")
	writeArtifact(t, m, sess.ID, "raw.txt", "just text\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secCanvas

	// artifacts sort by path: flow.mmd, raw.txt, readme.md.
	body := m.canvasBody(76, 24)
	if !strings.Contains(body, "Start ──▶ Done") {
		t.Fatalf("mermaid preview missing: %q", body)
	}
	m.HandleKey("j") // raw.txt
	if body := m.canvasBody(76, 24); !strings.Contains(body, "just text") {
		t.Fatalf("raw preview missing: %q", body)
	}
	m.HandleKey("j") // readme.md
	if body := m.canvasBody(76, 24); !strings.Contains(body, "Title") {
		t.Fatalf("markdown preview missing: %q", body)
	}
	// canvas of nothing degrades gracefully
	m2, _, _, _, _ := newSurface(t)
	if got := m2.canvasBody(76, 20); !strings.Contains(got, "no session open") {
		t.Fatalf("empty canvas = %q", got)
	}
}

func TestCanvasOpenInEditorSeam(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess, _ := st.Create("Docs", "", nil)
	writeArtifact(t, m, sess.ID, "a.md", "# A\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	opened := ""
	m.openInEditor = func(paths []string) bool {
		if len(paths) == 1 {
			opened = paths[0]
		}
		return true
	}
	m.open(sess.ID)
	m.sec = secCanvas
	m.HandleKey("e")
	if !strings.HasSuffix(opened, "a.md") {
		t.Fatalf("open seam got %q", opened)
	}
	// nil seam degrades with a named hint.
	m.openInEditor = nil
	m.HandleKey("e")
	if !strings.Contains(m.opErr, "unavailable") {
		t.Fatalf("nil seam hint = %q", m.opErr)
	}
}

func TestRemoveSessionConfirm(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess, _ := st.Create("Temp", "", nil)
	writeArtifact(t, m, sess.ID, "keep.md", "data")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("x")
	if m.form.kind != fRemoveConfirm {
		t.Fatal("expected remove confirm")
	}
	m.HandleKey("esc")
	if _, ok := st.Get(sess.ID); !ok {
		t.Fatal("esc removed the session")
	}
	m.HandleKey("x")
	m.HandleKey("enter")
	if _, ok := st.Get(sess.ID); ok {
		t.Fatal("session survived confirm")
	}
	if _, err := os.Stat(m.store.DirFor(sess.ID)); err != nil {
		t.Fatal("artifact folder was deleted")
	}
}

func TestNilDepsDegrade(t *testing.T) {
	m, ws, _, _, _ := newSurface(t)
	bare := New("0.1.0", ws, Deps{})
	bare.Resize(100, 30)
	if body := bare.sessionsBody(76); !strings.Contains(body, "unavailable") {
		t.Fatalf("nil store body = %q", body)
	}
	if bare.HandleKey("n") {
		t.Fatal("nil store accepted n")
	}
	if m.HandleKey("q") {
		t.Fatal("unknown key consumed")
	}
}

func TestParticipantsFloorAndRoles(t *testing.T) {
	m, _, st, crew, _ := newSurface(t)
	sess, err := st.CreateSession(ideation.CreateOptions{
		Name: "Round", Topic: "engines", Mode: ideation.ModeGroup,
		Agents: []string{"scout", "mason"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secParticipants

	// Cursor 0 is the moderator slot; row 1 is the first sorted agent.
	m.HandleKey("j")
	agent, ok := m.participantAt(m.cursors[secParticipants])
	if !ok || agent == "" {
		t.Fatalf("participant cursor = %d agent=%q ok=%v", m.cursors[secParticipants], agent, ok)
	}
	// f grants the floor and dispatches a turn.
	m.HandleKey("f")
	got, _ := st.Get(sess.ID)
	if got.CurrentSpeaker() != agent {
		t.Fatalf("floor = %q want %q", got.CurrentSpeaker(), agent)
	}
	if len(crew.handled) == 0 || !strings.Contains(crew.handled[len(crew.handled)-1].Text, "@"+agent) {
		t.Fatalf("floor grant did not dispatch: %+v", crew.handled)
	}
	// Invite a third agent.
	m.HandleKey("a")
	m.form.fields[0].runes = []rune("nova")
	m.HandleKey("enter")
	got, _ = st.Get(sess.ID)
	if len(got.Agents) != 3 {
		t.Fatalf("invite = %v", got.Agents)
	}
	// Remove the selected participant (confirm).
	m.HandleKey("x")
	if m.form.kind != fRemoveParticipant {
		t.Fatalf("x did not open remove: %v", m.form.kind)
	}
	m.HandleKey("enter")
	got, _ = st.Get(sess.ID)
	if got.Participates(agent) {
		t.Fatalf("participant not removed: %v", got.Agents)
	}
	// m makes the selected participant the moderator.
	sel, ok := m.participantAt(m.cursors[secParticipants])
	if !ok || sel == "" {
		t.Fatalf("no participant selected after removal: %d", m.cursors[secParticipants])
	}
	m.HandleKey("m")
	got, _ = st.Get(sess.ID)
	if got.Moderator != sel {
		t.Fatalf("moderator = %q want %q", got.Moderator, sel)
	}
}

func TestRoundTableFloorProtocol(t *testing.T) {
	m, _, st, crew, b := newSurface(t)
	sess, err := st.CreateSession(ideation.CreateOptions{
		Name: "Round", Topic: "storage", Mode: ideation.ModeGroup,
		Agents: []string{"scout", "mason"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secTranscript

	// Human addresses scout → floor moves to scout and a turn dispatches.
	m.chatPost("@scout propose two options")
	got, _ := st.Get(sess.ID)
	if got.CurrentSpeaker() != "scout" {
		t.Fatalf("after human post floor = %q", got.CurrentSpeaker())
	}
	if len(crew.handled) != 1 || !strings.Contains(crew.handled[0].Text, "@scout") {
		t.Fatalf("human dispatch = %+v", crew.handled)
	}

	// Scout replies addressing mason → floor hands to mason automatically.
	reply, err := b.Post(bus.Message{
		Channel: sess.Channel, Author: "scout",
		Text: "@mason what do you think?",
	})
	if err != nil {
		t.Fatal(err)
	}
	m.mirrorBus(reply)
	got, _ = st.Get(sess.ID)
	if got.CurrentSpeaker() != "mason" {
		t.Fatalf("floor hand-off = %q (turns %+v)", got.CurrentSpeaker(), got.Turns)
	}
	if len(crew.handled) != 2 || !strings.Contains(crew.handled[1].Text, "@mason") {
		t.Fatalf("hand-off dispatch = %+v", crew.handled)
	}

	// Mason replies unaddressed → floor returns to the moderator.
	reply2, _ := b.Post(bus.Message{Channel: sess.Channel, Author: "mason", Text: "Option A."})
	m.mirrorBus(reply2)
	got, _ = st.Get(sess.ID)
	if got.CurrentSpeaker() != "" {
		t.Fatalf("floor should return to moderator, got %q", got.CurrentSpeaker())
	}
	// The ordered record replays.
	if len(got.Turns) != 3 || got.Turns[0].Speaker != "scout" || got.Turns[1].Speaker != "mason" || got.Turns[2].Speaker != "" {
		t.Fatalf("turn order = %+v", got.Turns)
	}
	// Transcript carries ordered ids.
	body := m.chatBody(76, 24)
	if !strings.Contains(body, "#1") || !strings.Contains(body, "floor ") {
		t.Fatalf("transcript body = %q", body)
	}
}

func TestOneOnOneAutoDispatch(t *testing.T) {
	m, _, st, crew, _ := newSurface(t)
	sess, err := st.CreateSession(ideation.CreateOptions{
		Name: "Pair", Mode: ideation.ModeOneOnOne, Agents: []string{"scout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secTranscript
	m.chatPost("how should we cache this?")
	got, _ := st.Get(sess.ID)
	if got.CurrentSpeaker() != "scout" {
		t.Fatalf("1:1 floor = %q", got.CurrentSpeaker())
	}
	// requestTurn(posted) + the 1:1 invite both dispatch.
	if len(crew.handled) != 2 || !strings.Contains(crew.handled[1].Text, "@scout") {
		t.Fatalf("1:1 dispatch = %+v", crew.handled)
	}
}

func TestProposalsAcceptDecline(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	parent, _ := st.Create("Parent", "", []string{"scout"})
	if _, err := st.Propose("scout", "New idea", "explore", ideation.ModeGroup, "", []string{"scout"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Propose("mason", "Deep dive", "", ideation.ModeBreakout, parent.ID, nil); err != nil {
		t.Fatal(err)
	}
	m.sec = secSessions
	if rows := m.sessionRows(); len(rows) < 2 || rows[0].proposal == nil {
		t.Fatalf("proposals not first: %+v", rows)
	}

	// Accept proposal #1 (index 0) → a session opens.
	m.cursors[secSessions] = 0
	m.HandleKey("a")
	p1, _ := st.Get("new-idea")
	if p1.ID == "" {
		t.Fatalf("accept did not create the session; pending=%+v", st.PendingProposals())
	}
	if m.openID != "new-idea" || m.sec != secCanvas {
		t.Fatalf("accept state: openID=%q sec=%v", m.openID, m.sec)
	}
	// Decline the breakout proposal.
	m.sec = secSessions
	m.cursors[secSessions] = 0
	m.HandleKey("x")
	if len(st.PendingProposals()) != 0 {
		t.Fatalf("decline left a pending proposal: %+v", st.PendingProposals())
	}
	if _, ok := st.Get("deep-dive"); ok {
		t.Fatal("decline created a session")
	}
}

func TestSurfaceContract(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	if m.Meta().ID != "ideator" || m.Meta().Title != "Ideator" {
		t.Fatalf("meta = %+v", m.Meta())
	}
	for s, want := range map[sectionID]string{
		secSessions: "SESSIONS", secParticipants: "PARTICIPANTS",
		secCanvas: "CANVAS", secTranscript: "TRANSCRIPT",
	} {
		if got := s.label(); got != want {
			t.Fatalf("label(%d) = %q, want %q", s, got, want)
		}
	}
}
