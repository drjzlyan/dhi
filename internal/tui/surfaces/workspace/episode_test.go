package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func sampleEpisode() episode.Episode {
	return episode.Episode{
		Key: "task:fix-split", Agent: "dev", Turns: 2, CostUSD: 0.4182,
		TokensIn: 120, CacheRead: 8800, CacheWrite: 1080, Session: "S1",
		Plan: []episode.PlanStep{
			{Title: "Write a failing test for the remainder", Status: "done"},
			{Title: "Distribute the remainder one cent at a time to the first parties", Status: "doing"},
			{Title: "Open the PR", Status: "todo"},
			{Title: "Ask about rounding policy", Status: "blocked"},
		},
		Verify: "fail", VerifyCmd: "go test ./...", Iterations: 1,
	}
}

// F-067: the card pane's live-run block — plan, DHI's verdict, spend.
func TestEpisodeBlockGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	golden.Snapshot(t, "board_episode_block", strings.Join(renderEpisode(sampleEpisode(), 40), "\n"))
}

func TestVerifyLines(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	for verdict, want := range map[string]string{
		"pass": "✓ verified · go test ./...", "stuck": "✗ stuck — needs you", "": "",
	} {
		got := ansi.Strip(verifyLine(episode.Episode{Verify: verdict, VerifyCmd: "go test ./..."}))
		if !strings.Contains(got, want) || (want == "" && got != "") {
			t.Errorf("verify %q = %q, want %q", verdict, got, want)
		}
	}
}

// The block appears on the board only for the assignee's episode on
// this card.
func TestBoardShowsEpisode(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard
	if err := store.Create("fix-split", "Fix Split rounding", "dev", ""); err != nil {
		t.Fatal(err)
	}
	m.episodes = episode.Open(ws.Root)
	if out := ansi.Strip(m.boardBody(110, 34)); strings.Contains(out, "PLAN") {
		t.Fatal("no episode yet: no block")
	}
	if err := m.episodes.Put(sampleEpisode()); err != nil {
		t.Fatal(err)
	}
	out := ansi.Strip(m.boardBody(110, 34))
	for _, want := range []string{"PLAN 1/4", "Write a failing test", "verify failing", "2 turns · $0.4182 · 88% cached · session kept"} {
		if !strings.Contains(out, want) {
			t.Fatalf("board missing %q:\n%s", want, out)
		}
	}
}
