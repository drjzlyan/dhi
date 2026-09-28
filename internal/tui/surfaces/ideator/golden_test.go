package ideator

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

func goldenCompare(t *testing.T, name, actual string) {
	t.Helper()
	golden.Snapshot(t, name, actual)
}

// Goldens pin the ANSI-stripped layout of each section. Regenerate
// deliberately after an intentional visual change:
//
//	DHI_UPDATE_GOLDENS=1 go test ./internal/tui/surfaces/ideator/
func TestGoldenSessionsEmpty(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	goldenCompare(t, "sessions-empty", m.View())
}

func TestGoldenNewSessionModal(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	m.HandleKey("n")
	goldenCompare(t, "new-session-modal", m.View())
}

func TestGoldenSessionsProposal(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	if _, err := st.Create("Payment retries", "idempotency strategy", []string{"scout", "mason"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Propose("mason", "Deep dive", "narrow the retry policy", ideation.ModeBreakout, "payment-retries", nil); err != nil {
		t.Fatal(err)
	}
	goldenCompare(t, "sessions-proposal", m.View())
}

func TestGoldenParticipants(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess, err := st.CreateSession(ideation.CreateOptions{
		Name: "Round table", Topic: "storage engines", Mode: ideation.ModeGroup,
		Moderator: "scout", Agents: []string{"scout", "mason"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.GrantFloor(sess.ID, "mason"); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secParticipants
	goldenCompare(t, "participants", m.View())
}

func TestGoldenCanvasMarkdown(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess := createSession(t, m, "Docs", "", "")
	writeArtifact(t, m, sess.ID, "plan.md", "# Ideation plan\n\n## Goals\n\n- alternatives\n- review loop\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secCanvas
	goldenCompare(t, "canvas-markdown", m.View())
}

func TestGoldenCanvasMermaid(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess := createSession(t, m, "Flows", "", "")
	writeArtifact(t, m, sess.ID, "flow.mmd", "graph TD\n  A[Idea] --> B{Review}\n  B -->|yes| C[Done]\n  B -->|no| A\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secCanvas
	goldenCompare(t, "canvas-mermaid", m.View())
}

func TestGoldenTranscriptComposer(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	sess := createSession(t, m, "Ideas", "pick a storage engine", "scout")
	m.open(sess.ID)
	m.sec = secTranscript
	m.HandleKey("i")
	for _, r := range "@scout sketch the trade-offs, please" {
		m.HandleKey(string(r))
	}
	goldenCompare(t, "transcript-composer", m.View())
}

func TestGoldenRejectModal(t *testing.T) {
	m, _, st, _, _ := newSurface(t)
	sess := createSession(t, m, "Ideas", "", "")
	writeArtifact(t, m, sess.ID, "design.md", "# v1\n")
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secCanvas
	m.HandleKey("r")
	goldenCompare(t, "reject-modal", m.View())
}
