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

	// cursor nav
	if !m.HandleKey("j") || m.cursors[secSessions] != 1 {
		t.Fatalf("j did not move cursor: %d", m.cursors[secSessions])
	}
	if !m.HandleKey("k") || m.cursors[secSessions] != 0 {
		t.Fatal("k did not move cursor back")
	}
	// open scans the (empty) artifact folder and switches sections
	m.HandleKey("enter")
	if m.sec != secArtifacts || m.openID != a.ID {
		t.Fatalf("open: sec=%v openID=%q", m.sec, m.openID)
	}
	// esc returns to SESSIONS
	m.HandleKey("esc")
	if m.sec != secSessions {
		t.Fatal("esc did not return to SESSIONS")
	}
}

func TestCreateSessionFlow(t *testing.T) {
	m, _, _, crew, _ := newSurface(t)
	sess := createSession(t, m, "Payment retries", "idempotency", "scout, mason")
	if sess.ID != "payment-retries" || sess.Channel != "#ideation-payment-retries" {
		t.Fatalf("session = %+v", sess)
	}
	if got := m.sessions(); len(got) != 1 || got[0].ID != sess.ID {
		t.Fatalf("sessions = %+v", got)
	}
	if m.sec != secArtifacts || m.openID != sess.ID {
		t.Fatalf("post-create state: sec=%v openID=%q", m.sec, m.openID)
	}
	_ = crew

	// empty name is refused inside the modal (back on SESSIONS first)
	m.HandleKey("esc")
	m.HandleKey("n")
	m.form.fields[0].runes = nil
	m.HandleKey("enter")
	if m.form.err == "" {
		t.Fatal("empty name accepted")
	}
	m.HandleKey("esc")
}

func TestArtifactsLifecycle(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess, _ := st.Create("Ideas", "", []string{"scout"})
	writeArtifact(t, m, sess.ID, "design.md", "# Design\n\nContent here.\n")
	writeArtifact(t, m, sess.ID, "notes.md", "plain text\n")

	m.open(sess.ID)
	m.sec = secArtifacts
	if got := m.artifacts(); len(got) != 2 {
		t.Fatalf("artifacts = %+v", got)
	}

	// mark reviewed, then approve the other one
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
	// approved is terminal: reject refused, opErr set
	m.HandleKey("r")
	m.form.fields[0].runes = []rune("too late")
	m.HandleKey("enter")
	if m.form.kind != fReject || m.form.err == "" {
		t.Fatalf("terminal approve not enforced: %+v", m.form)
	}
	m.HandleKey("esc")

	// rescan flips rewritten files back to draft
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
	m.sec = secArtifacts

	m.HandleKey("r")
	if m.form.kind != fReject {
		t.Fatalf("expected fReject, got %v", m.form.kind)
	}
	// notes required
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

	// the revision request went to the session channel, mention first
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
	// drain the subscription the way the app would
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
	// unknown-session traffic is ignored
	other, _ := b.Post(bus.Message{Channel: "#general", Author: "scout", Text: "hi"})
	m.mirrorBus(other)
}

func TestPreviewMarkdownAndRaw(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess, _ := st.Create("Docs", "", nil)
	writeArtifact(t, m, sess.ID, "readme.md", "# Title\n\nSome **bold** text.\n")
	writeArtifact(t, m, sess.ID, "raw.txt", "just text\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secArtifacts

	m.HandleKey("enter") // preview raw.txt (sorts first)
	if m.sec != secPreview {
		t.Fatal("enter did not switch to PREVIEW")
	}
	if !strings.Contains(m.previewBody(76, 20), "just text") {
		t.Fatal("raw preview missing content")
	}
	// h returns to ARTIFACTS
	m.HandleKey("h")
	if m.sec != secArtifacts {
		t.Fatal("h did not return")
	}
	m.HandleKey("j") // readme.md
	m.HandleKey("enter")
	body := m.previewBody(76, 20)
	if !strings.Contains(body, "Title") {
		t.Fatalf("markdown preview missing heading: %q", body)
	}
	// preview of nothing degrades gracefully
	m2, _, _, _, _ := newSurface(t)
	if got := m2.previewBody(76, 20); !strings.Contains(got, "no artifact selected") {
		t.Fatalf("empty preview body = %q", got)
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
	if m.openID == sess.ID {
		t.Fatal("open id still points at removed session")
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

func TestChatComposeAndDispatch(t *testing.T) {
	m, _, st, crew, b := newSurface(t)
	sess, _ := st.Create("Ideas", "", []string{"scout", "mason"})
	m.open(sess.ID)
	m.sec = secChat

	// no session open → compose refused
	m2, _, _, _, _ := newSurface(t)
	m2.sec = secChat
	m2.HandleKey("i")
	if m2.chatFocus {
		t.Fatal("compose focused without a session")
	}

	m.HandleKey("i")
	if !m.chatFocus {
		t.Fatal("i did not focus composer")
	}
	for _, r := range "@scout @mason propose two storage alternatives" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	if len(m.chatInput) != 0 {
		t.Fatal("enter did not clear input")
	}
	m.HandleKey("esc")
	if m.chatFocus {
		t.Fatal("esc did not blur composer")
	}
	if len(crew.handled) != 1 || crew.handled[0].Text == "" {
		t.Fatalf("dispatch = %+v", crew.handled)
	}
	hist := b.History(sess.Channel, 0)
	if len(hist) != 1 || hist[0].Author != bus.Human {
		t.Fatalf("history = %+v", hist)
	}

	// agent reply arrives on the bus; the pump mirrors it and re-renders
	reply, err := b.Post(bus.Message{
		Channel: sess.Channel, Author: "scout",
		Text: "Option A: append-only log, Option B: b-tree",
	})
	if err != nil {
		t.Fatal(err)
	}
	m.mirrorBus(reply)
	body := m.chatBody(76, 20)
	if !strings.Contains(body, "Option A") {
		t.Fatalf("reply not rendered: %q", body)
	}
	// human messages render with their author tag too
	if !strings.Contains(body, "propose two storage") {
		t.Fatalf("own message not rendered: %q", body)
	}
}

func TestFullAcceptanceFlow(t *testing.T) {
	// F-004 acceptance: start a session, invite two agents, request
	// alternatives; both respond in chat and produce artifacts; reject
	// routes revision instructions back to the authoring agent.
	m, _, st, crew, _ := newSurface(t)
	sess := createSession(t, m, "Storage", "engines", "scout, mason")
	m.sec = secArtifacts

	// agents "respond" and produce artifacts
	writeArtifact(t, m, sess.ID, "option-log.md", "# Log\n")
	writeArtifact(t, m, sess.ID, "option-btree.md", "# B-tree\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	// option-btree.md sorts first; scout authored it
	if err := st.ClaimAuthor(sess.ID, "option-btree.md", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAuthor(sess.ID, "option-log.md", "mason"); err != nil {
		t.Fatal(err)
	}

	// human rejects scout's option with notes → routes to scout only
	m.HandleKey("r")
	m.form.fields[0].runes = []rune("compare against write throughput")
	m.HandleKey("enter")
	if len(crew.handled) != 1 {
		t.Fatalf("crew dispatches = %d", len(crew.handled))
	}
	if !strings.HasPrefix(crew.handled[0].Text, "@scout ") {
		t.Fatalf("revision routed to %q: %q", "", crew.handled[0].Text)
	}
	if crew.handled[0].Channel != sess.Channel {
		t.Fatalf("revision went to %q", crew.handled[0].Channel)
	}

	// scout revises the file; rescan flips it back to draft
	writeArtifact(t, m, sess.ID, "option-btree.md", "# B-tree v2\n\nthroughput table\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	a, _ := st.Artifact(sess.ID, "option-btree.md")
	if a.Status != ideation.StatusDraft {
		t.Fatalf("revised status = %q", a.Status)
	}

	// approve mason's untouched artifact
	m.HandleKey("j")
	m.HandleKey("a")
	a2, _ := st.Artifact(sess.ID, "option-log.md")
	if a2.Status != ideation.StatusApproved {
		t.Fatalf("approve failed: %+v", a2)
	}
	_ = crew
}

func TestSurfaceContract(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	if m.Meta().ID != "ideator" || m.Meta().Title != "Ideator" {
		t.Fatalf("meta = %+v", m.Meta())
	}
}
