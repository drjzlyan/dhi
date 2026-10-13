package memory

import (
	"os"
	"path/filepath"
	"strconv"
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

// F-078: a journal stays bounded — old entries move to the monthly
// archive and a digest line each; the newest stay in the journal.
func TestJournalRotatesIntoArchiveAndDigest(t *testing.T) {
	s := fixture(t)
	s.SetJournalMax(50)
	for i := 0; i < 70; i++ {
		if err := s.Append("scout", "note", "entry "+strconv.Itoa(i)+" "+strings.Repeat("words ", 8)); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.Journal("scout", 0)
	if err != nil || len(all) < 50 || len(all) > 62 || !strings.HasPrefix(all[len(all)-1].Text, "entry 69") {
		t.Fatalf("journal = %d entries, last %q, %v", len(all), all[len(all)-1].Text, err)
	}
	d, _ := s.Digest("scout")
	if !strings.Contains(d, ": entry 0 ") || strings.Contains(d, "entry 69") {
		t.Fatalf("digest:\n%s", d)
	}
	dir, _ := s.dir("scout")
	if m, _ := filepath.Glob(filepath.Join(dir, "journal-*.jsonl")); len(m) != 1 {
		t.Fatalf("archive files = %v", m)
	}
	if tail, _ := s.Journal("scout", 3); len(tail) != 3 || !strings.HasPrefix(tail[2].Text, "entry 69") {
		t.Fatalf("tail = %+v", tail)
	}
}
