package episode

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
)

// The session's size is the last call's prompt, not the run's sum.
func TestFoldPrefersTheLastCallsContext(t *testing.T) {
	e := Fold(Episode{}, tasks.Run{Status: tasks.RunOK, TokensIn: 50, CacheRead: 190000, Context: 13005}, "claude", "/w", "s", 1)
	if e.LastContext != 13005 {
		t.Fatalf("LastContext = %d", e.LastContext)
	}
	e = Fold(Episode{}, tasks.Run{Status: tasks.RunOK, TokensIn: 50, CacheRead: 1000}, "codex", "/w", "s", 1)
	if e.LastContext != 1050 {
		t.Fatalf("fallback LastContext = %d", e.LastContext)
	}
}
