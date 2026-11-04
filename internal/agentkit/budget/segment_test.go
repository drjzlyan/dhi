package budget

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/jsonl"
)

func TestLedgerRotationKeepsTotals(t *testing.T) {
	root := t.TempDir()
	l := Open(root)
	l.SegmentBytes = 200
	now := time.Date(2026, 10, 13, 15, 0, 0, 0, time.Local)
	for i := 0; i < 10; i++ {
		if err := l.Append(Entry{At: now, Agent: "dev", Task: "fix", Tokens: 100, CostUSD: 0.1}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(jsonl.SegmentPaths(filepath.Join(root, ".dhi", "spend.jsonl"))); n < 2 {
		t.Fatalf("segments = %d, want rotation", n)
	}
	es, err := l.Entries()
	if err != nil || len(es) != 10 {
		t.Fatalf("entries = %d %v", len(es), err)
	}
	s := Tally(es, "dev", "fix", DayStart(now))
	if s.TaskTokens != 1000 || s.TaskUSD < 0.999 || s.TaskUSD > 1.001 || s.WorkspaceDayUSD < 0.999 {
		t.Fatalf("tally across boundary = %+v", s)
	}
}

// A ledger written before rotation existed reads and keeps counting.
func TestLegacyLedgerReads(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile("testdata/spend-v1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dhi", "spend.jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	l := Open(root)
	l.SegmentBytes = 50 // legacy file is over the line: next append rotates it
	if err := l.Append(Entry{At: time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC), Agent: "dev", Task: "fix", Tokens: 10, CostUSD: 0.5}); err != nil {
		t.Fatal(err)
	}
	es, _ := l.Entries()
	if len(es) != 3 {
		t.Fatalf("entries = %d", len(es))
	}
	if s := Tally(es, "dev", "fix", time.Time{}); s.TaskTokens != 1010 {
		t.Fatalf("tally = %+v", s)
	}
}
