package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestSearchCachesAndInvalidates(t *testing.T) {
	s := fixture(t, Auto)
	e := publish(t, s, Contribution{Title: "Deploy steps", Body: "run the release script", Author: "dev"}, time.Now())
	publish(t, s, Contribution{Title: "Style", Body: "tabs not spaces", Author: "dev"}, time.Now())
	ctx := context.Background()

	if h, _ := s.Search(ctx, "release script", 5); len(h) == 0 || h[0].Entry.ID != e.ID {
		t.Fatalf("first search = %+v", h)
	}
	reads, builds := s.bodyReads, s.indexBuilds
	if reads != 2 || builds != 1 {
		t.Fatalf("cold: reads %d builds %d", reads, builds)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Search(ctx, "tabs", 5); err != nil {
			t.Fatal(err)
		}
	}
	if s.bodyReads != reads || s.indexBuilds != builds {
		t.Fatalf("warm searches re-read: reads %d builds %d", s.bodyReads, s.indexBuilds)
	}

	// Editing one body file re-reads that file only and rebuilds the index.
	p := filepath.Join(s.root, filepath.FromSlash(e.File))
	if err := os.WriteFile(p, []byte("# Deploy steps\n\nnow use the zebra pipeline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, _ := s.Search(ctx, "zebra pipeline", 5)
	if len(h) == 0 || h[0].Entry.ID != e.ID {
		t.Fatalf("edit not seen: %+v", h)
	}
	if s.bodyReads != reads+1 || s.indexBuilds != builds+1 {
		t.Fatalf("after edit: reads %d builds %d", s.bodyReads, s.indexBuilds)
	}

	// A new entry invalidates too.
	n := publish(t, s, Contribution{Title: "Quokka", Body: "marsupial notes", Author: "dev"}, time.Now())
	if h, _ := s.Search(ctx, "marsupial", 5); len(h) == 0 || h[0].Entry.ID != n.ID {
		t.Fatalf("new entry not seen: %+v", h)
	}
}

// A KB written by an older version (index.json + entry files, no cache)
// opens and searches unchanged.
func TestLegacyKBFixtureSearches(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	kb := filepath.Join(ws.Root, workspace.DirKnowledge)
	if err := os.MkdirAll(filepath.Join(kb, "entries"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.json", "entries/deploy-abc123.md"} {
		data, err := os.ReadFile(filepath.Join("testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(kb, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(ws, Review, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.Search(context.Background(), "release script", 3)
	if err != nil || len(h) != 1 || h[0].Entry.ID != "deploy-abc123" {
		t.Fatalf("legacy search = %+v %v", h, err)
	}
}
