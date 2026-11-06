package bus

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/jsonl"
)

func TestChannelRotatesAndReadsAcrossSegments(t *testing.T) {
	ws := fixtureWS(t)
	b, _ := Open(ws)
	b.SegmentBytes = 300
	for i := 0; i < 12; i++ {
		if _, err := b.Post(Message{Channel: "#dev", Author: Human, Text: fmt.Sprintf("message number %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(ws.Root, ".dhi", "channels", "channels", "dev.jsonl")
	if n := len(jsonl.SegmentPaths(path)); n < 2 {
		t.Fatalf("segments = %d, want rotation", n)
	}
	h := b.History("#dev", 0)
	if len(h) != 12 {
		t.Fatalf("history = %d, want 12", len(h))
	}
	for i, m := range h {
		if m.ID != int64(i+1) {
			t.Fatalf("order broken at %d: id %d", i, m.ID)
		}
	}
	// The window reaches into older segments when the active file is short.
	if r := b.Recent("#dev", 0, 10); len(r) != 10 || r[9].ID != 12 || r[0].ID != 3 {
		t.Fatalf("recent = %d msgs", len(r))
	}
	if got := b.Channels(); len(got) != 1 || got[0] != "#dev" {
		t.Fatalf("segments leaked into channels: %v", got)
	}
	// Reopen: the ID counter clears every segment, history is intact.
	b2, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := b2.Post(Message{Channel: "#dev", Author: Human, Text: "after reopen"})
	if m.ID != 13 || len(b2.History("#dev", 0)) != 13 {
		t.Fatalf("reopen: id %d, history %d", m.ID, len(b2.History("#dev", 0)))
	}
}

// A channel file written before segmentation existed reads unchanged.
func TestLegacyChannelFileReads(t *testing.T) {
	ws := fixtureWS(t)
	data, err := os.ReadFile("testdata/general-v1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(ws.Root, ".dhi", "channels", "channels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "general.jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if h := b.History("#general", 0); len(h) != 1 || h[0].Text != "hello" {
		t.Fatalf("history = %+v", h)
	}
	if b.ReplyCount("#general", 1) != 1 {
		t.Fatal("reply count lost")
	}
	b.SegmentBytes = 50 // the old file is over the line: next post rotates it
	m, _ := b.Post(Message{Channel: "#general", Author: Human, Text: "next"})
	if m.ID != 3 || len(b.All("#general")) != 3 {
		t.Fatalf("id %d, all %d", m.ID, len(b.All("#general")))
	}
}
