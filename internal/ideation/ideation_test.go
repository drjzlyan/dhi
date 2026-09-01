package ideation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func newStore(t *testing.T) (*Store, *workspace.Workspace) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatalf("member dir: %v", err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatalf("workspace.Create: %v", err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatalf("workspace.Load: %v", err)
	}
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s, err := Open(ws)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.now = func() time.Time { return clock }
	return s, ws
}

func writeArtifact(t *testing.T, s *Store, id, rel, content string) {
	t.Helper()
	abs := s.ArtifactPath(id, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
}

func TestCreateRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	sess, err := s.Create("Payment retry design", "idempotency strategy", []string{"scout", "mason", "scout"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ID != "payment-retry-design" {
		t.Fatalf("id = %q", sess.ID)
	}
	if sess.Channel != "#ideation-payment-retry-design" {
		t.Fatalf("channel = %q", sess.Channel)
	}
	if len(sess.Agents) != 2 || sess.Agents[0] != "mason" || sess.Agents[1] != "scout" {
		t.Fatalf("agents = %v", sess.Agents)
	}
	if _, err := os.Stat(s.DirFor(sess.ID)); err != nil {
		t.Fatalf("artifact folder missing: %v", err)
	}

	// Reopen: card round-trips.
	s2, err := Open(s.ws)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := s2.Get(sess.ID)
	if !ok {
		t.Fatal("session lost after reopen")
	}
	if got.Name != "Payment retry design" || got.Topic != "idempotency strategy" || got.Channel != sess.Channel {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Agents) != 2 {
		t.Fatalf("agents round-trip = %v", got.Agents)
	}
}

func TestCreateValidation(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Create("   ", "", nil); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := s.Create("Payment retry design", "", nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := s.Create("Payment retry design", "", nil); err == nil {
		t.Error("duplicate id accepted")
	}
}

func TestScanMergesFilesystem(t *testing.T) {
	s, _ := newStore(t)
	sess, _ := s.Create("Ideas", "", nil)

	// Empty folder → no artifacts.
	if err := s.Scan(sess.ID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got, _ := s.Get(sess.ID); len(got.Artifacts) != 0 {
		t.Fatalf("expected no artifacts, got %+v", got.Artifacts)
	}

	// Two new files → two drafts.
	writeArtifact(t, s, sess.ID, "design.md", "# v1")
	writeArtifact(t, s, sess.ID, "notes/api.md", "api notes")
	if err := s.Scan(sess.ID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got, _ := s.Get(sess.ID)
	if len(got.Artifacts) != 2 {
		t.Fatalf("artifacts = %+v", got.Artifacts)
	}
	if got.Artifacts[0].Path != "design.md" || got.Artifacts[1].Path != "notes/api.md" {
		t.Fatalf("paths not sorted: %+v", got.Artifacts)
	}
	for _, a := range got.Artifacts {
		if a.Status != StatusDraft || a.Hash == "" {
			t.Fatalf("bad scan artifact: %+v", a)
		}
	}

	// Approve one, claim authors on both.
	if err := s.ClaimAuthor(sess.ID, "design.md", "scout"); err != nil {
		t.Fatalf("ClaimAuthor: %v", err)
	}
	if err := s.Approve(sess.ID, "design.md"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := s.ClaimAuthor(sess.ID, "notes/api.md", "mason"); err != nil {
		t.Fatalf("ClaimAuthor: %v", err)
	}

	// Unchanged rescan keeps statuses.
	if err := s.Scan(sess.ID); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	got, _ = s.Get(sess.ID)
	if got.Artifacts[0].Status != StatusApproved || got.Artifacts[0].Author != "scout" {
		t.Fatalf("approved artifact mutated: %+v", got.Artifacts[0])
	}

	// Rewriting the approved file flips it back to draft, keeps author.
	writeArtifact(t, s, sess.ID, "design.md", "# v2 — revised")
	if err := s.Scan(sess.ID); err != nil {
		t.Fatalf("rescan after rewrite: %v", err)
	}
	got, _ = s.Get(sess.ID)
	if got.Artifacts[0].Status != StatusDraft || got.Artifacts[0].Author != "scout" {
		t.Fatalf("rewrite did not flip to draft: %+v", got.Artifacts[0])
	}

	// Deleting a file drops its record.
	if err := os.Remove(s.ArtifactPath(sess.ID, "notes/api.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := s.Scan(sess.ID); err != nil {
		t.Fatalf("rescan after delete: %v", err)
	}
	got, _ = s.Get(sess.ID)
	if len(got.Artifacts) != 1 || got.Artifacts[0].Path != "design.md" {
		t.Fatalf("deleted artifact still recorded: %+v", got.Artifacts)
	}
}

func TestApproveRejectLifecycle(t *testing.T) {
	s, _ := newStore(t)
	sess, _ := s.Create("Auth", "", nil)
	writeArtifact(t, s, sess.ID, "auth.md", "content")
	if err := s.Scan(sess.ID); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if err := s.Reject(sess.ID, "auth.md", ""); err == nil {
		t.Error("reject without notes accepted")
	}
	if err := s.Reject(sess.ID, "auth.md", "threat-model the token refresh path"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	a, _ := s.Artifact(sess.ID, "auth.md")
	if a.Status != StatusRejected || a.Notes != "threat-model the token refresh path" {
		t.Fatalf("rejected artifact: %+v", a)
	}
	// Round-trip through disk.
	s2, _ := Open(s.ws)
	a2, _ := s2.Artifact(sess.ID, "auth.md")
	if a2.Status != StatusRejected || a2.Notes != a.Notes {
		t.Fatalf("rejection did not persist: %+v", a2)
	}

	if err := s.MarkReviewed(sess.ID, "auth.md"); err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}
	if err := s.Approve(sess.ID, "auth.md"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// Approved is terminal.
	if err := s.Reject(sess.ID, "auth.md", "too late"); err == nil {
		t.Error("approved artifact was rejected")
	}
	if err := s.MarkReviewed(sess.ID, "auth.md"); err == nil {
		t.Error("approved artifact moved to reviewed")
	}

	// Unknown artifact/session.
	if err := s.Approve(sess.ID, "ghost.md"); err == nil {
		t.Error("unknown artifact approved")
	}
	if err := s.Approve("ghost", "auth.md"); err == nil {
		t.Error("unknown session approved")
	}
}

func TestRemoveKeepsFolder(t *testing.T) {
	s, _ := newStore(t)
	sess, _ := s.Create("Temp", "", nil)
	writeArtifact(t, s, sess.ID, "keep.md", "data")
	if err := s.Scan(sess.ID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if err := s.Remove(sess.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := s.Get(sess.ID); ok {
		t.Fatal("session survived Remove")
	}
	if _, err := os.Stat(s.DirFor(sess.ID)); err != nil {
		t.Fatalf("artifact folder was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.ws.Root, Dir, sess.ID+".toml")); !os.IsNotExist(err) {
		t.Fatalf("card survived Remove: %v", err)
	}
	if err := s.Remove(sess.ID); err == nil {
		t.Error("removing unknown session succeeded")
	}
}

func TestMalformedCardsWarn(t *testing.T) {
	_, ws := newStore(t)
	// Unknown key + bad schema + bad channel.
	bad := map[string]string{
		"broken.toml":    "schema = 1\nname = \"X\"\nchannel = \"#x\"\nwibble = 1\n",
		"oldschema.toml": "schema = 99\nname = \"X\"\nchannel = \"#x\"\n",
		"badchan.toml":   "schema = 1\nname = \"X\"\nchannel = \"not a channel\"\n",
	}
	for name, body := range bad {
		if err := os.WriteFile(filepath.Join(ws.Root, Dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write card: %v", err)
		}
	}
	s2, err := Open(ws)
	if err != nil {
		t.Fatalf("Open with malformed cards: %v", err)
	}
	if len(s2.Sessions()) != 0 {
		t.Fatalf("malformed cards loaded: %+v", s2.Sessions())
	}
	if len(s2.Warnings()) != len(bad) {
		t.Fatalf("warnings = %v", s2.Warnings())
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Payment retry design": "payment-retry-design",
		"  Spaces   and --  ":  "spaces-and",
		"!!!":                  "session",
		"CamelCase_API.v2":     "camelcase_api.v2",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSubscribePings(t *testing.T) {
	s, _ := newStore(t)
	ch, cancel := s.Subscribe()
	defer cancel()
	if _, err := s.Create("Pings", "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	select {
	case c := <-ch:
		if c.Kind != SessionCreated {
			t.Fatalf("change = %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("no change event")
	}
}

func TestScanOnUnknownSession(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Scan("ghost"); err == nil {
		t.Error("scan of unknown session succeeded")
	}
}
