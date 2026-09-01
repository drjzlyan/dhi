package ideator

import (
	"testing"

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

func TestGoldenArtifactsList(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	sess := createSession(t, m, "Design Alternatives", "storage engines", "scout, mason")
	writeArtifact(t, m, sess.ID, "design-sql.md", "# SQL first\n\nBoring but proven.\n")
	writeArtifact(t, m, sess.ID, "design-log.md", "# Log-structured\n\nAppend-only.\n")
	if err := m.store.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.store.ClaimAuthor(sess.ID, "design-sql.md", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := m.store.ClaimAuthor(sess.ID, "design-log.md", "mason"); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secArtifacts
	goldenCompare(t, "artifacts-list", m.View())
}

func TestGoldenPreviewMarkdown(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	sess := createSession(t, m, "Docs", "", "")
	writeArtifact(t, m, sess.ID, "plan.md", "# Ideation plan\n\n## Goals\n\n- alternatives\n- review loop\n")
	if err := m.store.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secArtifacts
	m.HandleKey("enter")
	goldenCompare(t, "preview-markdown", m.View())
}

func TestGoldenChatComposer(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	sess := createSession(t, m, "Ideas", "pick a storage engine", "scout")
	m.open(sess.ID)
	m.sec = secChat
	m.HandleKey("i")
	for _, r := range "@scout sketch the trade-offs, please" {
		m.HandleKey(string(r))
	}
	goldenCompare(t, "chat-composer", m.View())
}

func TestGoldenRejectModal(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	sess := createSession(t, m, "Ideas", "", "")
	writeArtifact(t, m, sess.ID, "design.md", "# v1\n")
	if err := m.store.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.open(sess.ID)
	m.sec = secArtifacts
	m.HandleKey("r")
	goldenCompare(t, "reject-modal", m.View())
}
