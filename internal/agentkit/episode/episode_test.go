package episode

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/tasks"
)

func TestKey(t *testing.T) {
	if got := Key("fix-login", "#general", 5); got != "task:fix-login" {
		t.Fatalf("task key = %q", got)
	}
	if got := Key("", "dm:dev", 7); got != "thread:dm:dev:7" {
		t.Fatalf("thread key = %q", got)
	}
}

func TestFoldAccumulates(t *testing.T) {
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	e := Episode{Key: "task:x", Agent: "dev"}
	e = Fold(e, tasks.Run{Status: tasks.RunOK, Summary: "did a", TokensIn: 10, TokensOut: 5, CacheRead: 100, CostUSD: 0.1, Finished: at}, "claude", "/w", "s1", 4)
	e = Fold(e, tasks.Run{Status: tasks.RunError, Error: "exit 2", TokensIn: -1, TokensOut: -1, Finished: at}, "claude", "/w", "", 3)
	if e.Turns != 2 || e.TokensIn != 10 || e.CacheRead != 100 || e.CostUSD != 0.1 {
		t.Fatalf("totals = %+v", e)
	}
	if e.Session != "" || e.LastStatus != "error" || e.LastSummary != "exit 2" {
		t.Fatalf("a failed fresh run must drop the session and record the error: %+v", e)
	}
	if e.SeenID != 4 {
		t.Fatalf("SeenID must never move backwards: %d", e.SeenID)
	}
}

func TestResumable(t *testing.T) {
	e := Episode{Session: "s1", CLI: "claude", Workdir: "/w"}
	if !e.Resumable("claude", "/w") || e.Resumable("codex", "/w") || e.Resumable("claude", "/other") {
		t.Fatal("resume must require the same CLI and workdir")
	}
	if (Episode{CLI: "claude", Workdir: "/w"}).Resumable("claude", "/w") {
		t.Fatal("no session, no resume")
	}
}

func TestRender(t *testing.T) {
	if (Episode{}).Render() != "" {
		t.Fatal("a first turn renders nothing")
	}
	got := Episode{Turns: 2, LastStatus: "ok", LastSummary: "added Sum"}.Render()
	if !strings.Contains(got, "2 earlier turns; the last one ended ok: added Sum.") {
		t.Fatalf("render = %q", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := Open(t.TempDir())
	if _, ok, err := s.Get("dev", "task:x"); ok || err != nil {
		t.Fatalf("empty store: %v %v", ok, err)
	}
	old := Episode{Key: "thread:#general:3", Agent: "dev", Updated: time.Unix(10, 0)}
	cur := Episode{Key: "task:x", Agent: "dev", Session: "s1", Updated: time.Unix(20, 0)}
	for _, e := range []Episode{old, cur} {
		if err := s.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := s.Get("dev", "task:x")
	if !ok || err != nil || got.Session != "s1" {
		t.Fatalf("Get = %+v %v %v", got, ok, err)
	}
	list, err := s.List("dev")
	if err != nil || len(list) != 2 || list[0].Key != "task:x" {
		t.Fatalf("List = %+v %v", list, err)
	}
	if err := s.Put(Episode{Key: "k"}); err == nil {
		t.Fatal("an episode without an agent must refuse")
	}
}
