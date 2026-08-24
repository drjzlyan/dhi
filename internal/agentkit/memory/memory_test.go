package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return Open(ws)
}

func TestJournalRoundTripAndLimit(t *testing.T) {
	s := fixture(t)
	for i, k := range []string{"turn", "lesson", "turn"} {
		if err := s.Append("scout", k, strings.Repeat("e", i+1)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	all, err := s.Journal("scout", 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("journal = %d entries err %v", len(all), err)
	}
	recent, err := s.Journal("scout", 2)
	if err != nil || len(recent) != 2 || recent[0].Text != "ee" {
		t.Errorf("limit window wrong: %+v err %v", recent, err)
	}
	for _, e := range all {
		if e.At.IsZero() || e.Kind == "" {
			t.Errorf("entry not stamped: %+v", e)
		}
	}
}

func TestNotesAtomicWrite(t *testing.T) {
	s := fixture(t)
	got, err := s.ReadNotes("scout")
	if err != nil || got != "" {
		t.Fatalf("empty notes = %q err %v", got, err)
	}
	if err := s.WriteNotes("scout", "# facts\n- v2 faster\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, _ = s.ReadNotes("scout"); !strings.HasPrefix(got, "# facts") {
		t.Errorf("notes = %q", got)
	}
	if _, err := os.Stat(filepath.Join(s.root, "agents", "scout", ".notes.tmp")); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
}

func TestBadAgentIDRejected(t *testing.T) {
	s := fixture(t)
	if err := s.Append("../evil", "k", "t"); err == nil {
		t.Error("traversal id accepted")
	}
	if _, err := s.Journal("../evil", 0); err == nil {
		t.Error("traversal id accepted for journal")
	}
}

func TestAppendValidation(t *testing.T) {
	s := fixture(t)
	if err := s.Append("scout", "", "text"); err == nil {
		t.Error("empty kind accepted")
	}
	if err := s.Append("scout", "kind", "  "); err == nil {
		t.Error("blank text accepted")
	}
}
