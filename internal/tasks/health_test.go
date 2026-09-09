package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCheckRunsCard(t *testing.T, root, slug, body string) {
	t.Helper()
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRunsEmptyAndAbsent(t *testing.T) {
	if got := CheckRuns(t.TempDir()); got != nil {
		t.Errorf("absent dir = %v, want nil", got)
	}
}

func TestCheckRunsHealthy(t *testing.T) {
	root := t.TempDir()
	writeCheckRunsCard(t, root, "ship-it", `schema = 1
title = "Ship"
status = "active"
created_at = 2026-01-01T00:00:00Z
updated_at = 2026-01-01T00:00:00Z

[[run]]
id = "run-1"
agent = "alice"
runtime = "cli:claude"
model = "opus"
attempt = 0
started = 2026-01-01T00:00:00Z
finished = 2026-01-01T01:00:00Z
status = "ok"
exit = 0
summary = "done"
tokens_in = 100
tokens_out = 50
cost_usd = 0.01
cost = true
transcript = ".dhi/agents/alice/runs/run-1-0.jsonl"
`)
	// the referenced transcript dir must exist for the card to be healthy
	if err := os.MkdirAll(filepath.Join(root, ".dhi", "agents", "alice", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := CheckRuns(root); len(got) != 0 {
		t.Errorf("healthy card warns: %v", got)
	}
}

func TestCheckRunsMalformedStatusNamesLine(t *testing.T) {
	root := t.TempDir()
	writeCheckRunsCard(t, root, "ship-it", `schema = 1
title = "Ship"
status = "active"
created_at = 2026-01-01T00:00:00Z
updated_at = 2026-01-01T00:00:00Z

[[run]]
id = "run-1"
agent = "alice"
started = 2026-01-01T00:00:00Z
finished = 2026-01-01T01:00:00Z
status = "maybe"
tokens_in = -1
tokens_out = -1
`)
	got := CheckRuns(root)
	if len(got) != 1 {
		t.Fatalf("warnings = %v, want 1", got)
	}
	msg := got[0]
	// names the card + the exact line of the offending status value
	if !strings.Contains(msg, "ship-it") || !strings.Contains(msg, "@L12") ||
		!strings.Contains(msg, `bad status "maybe"`) {
		t.Errorf("warning = %q, want card + @L12 + bad status named", msg)
	}
}

func TestCheckRunsMissingTranscriptDirWarns(t *testing.T) {
	root := t.TempDir()
	writeCheckRunsCard(t, root, "ship-it", `schema = 1
title = "Ship"
status = "active"
created_at = 2026-01-01T00:00:00Z
updated_at = 2026-01-01T00:00:00Z

[[run]]
id = "run-1"
agent = "alice"
started = 2026-01-01T00:00:00Z
finished = 2026-01-01T01:00:00Z
status = "ok"
tokens_in = -1
tokens_out = -1
transcript = ".dhi/agents/alice/runs/run-1-0.jsonl"
`)
	got := CheckRuns(root)
	found := false
	for _, w := range got {
		if strings.Contains(w, "transcript dir") && strings.Contains(w, "alice/runs") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one naming the missing transcript dir", got)
	}
}

func TestCheckRunsCardGarbageIsOutOfScope(t *testing.T) {
	// a card with no [[run]] block at all passes runs/store (kanban
	// health owns card-level warnings).
	root := t.TempDir()
	writeCheckRunsCard(t, root, "nada", `schema = 1
title = "No runs"
status = "backlog"
created_at = 2026-01-01T00:00:00Z
updated_at = 2026-01-01T00:00:00Z
`)
	if got := CheckRuns(root); len(got) != 0 {
		t.Errorf("run-less card warns: %v", got)
	}
}
