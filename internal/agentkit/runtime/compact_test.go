package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
)

// F-135: /compact writes a summary on agents.reflect_model, drops the CLI
// session, and the next turn starts fresh from the summary with only the
// messages after it.
func TestCompactSummarisesAndNextTurnStartsFromSummary(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Resume: true, MaxIterations: 3, ReflectModel: "cheap-m"} }
	root := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout first ask: remember kiwi"})
	if err := h.rt.Turn(context.Background(), "scout", root); err != nil {
		t.Fatal(err)
	}
	st, key := h.rt.cfg.Episodes, episode.Key("", "#general", root.ID)
	before, _, _ := st.Get("scout", key)
	if before.Session != "S1" {
		t.Fatalf("setup: %+v", before)
	}

	if err := h.rt.Compact(context.Background(), "scout", "#general", root.ID); err != nil {
		t.Fatal(err)
	}
	if args := spawnArgs(t, h, 2); !strings.Contains(args, "--model\ncheap-m\n") || !strings.Contains(args, "Compact this conversation") {
		t.Fatalf("the summary must run on agents.reflect_model:\n%s", args)
	}
	ep, _, _ := st.Get("scout", key)
	if ep.Summary != "reply 2" || ep.Session != "" || ep.LastContext != 0 || ep.SummaryUpTo == 0 {
		t.Fatalf("episode after compact: %+v", ep)
	}
	if ep.Turns != before.Turns {
		t.Fatalf("a compaction is not a turn: %d -> %d", before.Turns, ep.Turns)
	}

	next := mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: bus.Human, Text: "@scout second ask"})
	if err := h.rt.Turn(context.Background(), "scout", next); err != nil {
		t.Fatal(err)
	}
	args := spawnArgs(t, h, 3)
	if strings.Contains(args, "--resume") {
		t.Fatalf("the compacted session must not resume:\n%s", args)
	}
	if !strings.Contains(args, "Summary of the conversation so far (compacted)") || !strings.Contains(args, "reply 2") {
		t.Fatalf("the summary must lead the fresh turn:\n%s", args)
	}
	if strings.Contains(args, "remember kiwi") {
		t.Fatalf("messages the summary covers must not be replayed:\n%s", args)
	}
}

func TestCompactRefusesByName(t *testing.T) {
	h := episodeHarness(t)
	root := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout hi"})
	if err := h.rt.Compact(context.Background(), "scout", "#general", root.ID); err == nil || !strings.Contains(err.Error(), "no conversation yet") {
		t.Fatalf("err = %v", err)
	}
	if hasPrefix(h.bus.History("#general", root.ID), "could not compact: no conversation yet") == nil {
		t.Fatal("the refusal must be named in the thread")
	}
	if err := h.rt.Compact(context.Background(), "ghost", "#general", root.ID); err == nil {
		t.Fatal("unknown agent must refuse")
	}
}
