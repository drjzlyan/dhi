package clirun

import (
	"fmt"
	"strings"
	"testing"
)

// The failover list is closed: exactly these causes may move a run to the
// fallback engine, nothing else (ADR-0048).
func TestFailureFailoverClosedList(t *testing.T) {
	all := []Failure{FailAuth, FailRateLimit, FailOverflow, FailTimeout, FailStopped, FailGuard,
		FailNoFinal, FailStall, FailCrash, FailNotInstalled, FailUnavailable}
	want := map[Failure]bool{FailAuth: true, FailRateLimit: true, FailNotInstalled: true, FailUnavailable: true}
	for _, f := range all {
		if got := f.Failover(); got != want[f] {
			t.Errorf("%s.Failover() = %v, want %v", f, got, want[f])
		}
	}
	if FailNotInstalled.Retryable() {
		t.Error("a missing binary cannot be fixed by retrying")
	}
}

func TestPickFallback(t *testing.T) {
	cases := []struct {
		name      string
		primary   string
		installed []string
		want      string
		note      string
	}{
		{"fixed order wins over input order", "claude", []string{"copilot", "codex", "opencode"}, "codex", ""},
		{"excludes the primary", "codex", []string{"codex", "claude"}, "claude", ""},
		{"primary is first in order, next is picked", "claude", []string{"claude", "cursor-agent", "antigravity"}, "cursor-agent", ""},
		{"unlisted CLIs sort after, alphabetically", "claude", []string{"zeta", "alpha"}, "alpha", ""},
		{"listed beats unlisted", "claude", []string{"alpha", "antigravity"}, "antigravity", ""},
		{"only the primary installed", "claude", []string{"claude"}, "", "claude is the only installed CLI"},
		{"none installed", "claude", nil, "", "claude is the only installed CLI"},
		{"inherited engine, none installed", "", nil, "", "no CLI is installed"},
		{"inherited engine, any installed", "", []string{"codex"}, "codex", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, note := PickFallback(c.primary, c.installed)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if c.note == "" && note != "" || !strings.Contains(note, c.note) {
				t.Fatalf("note = %q, want it to contain %q", note, c.note)
			}
		})
	}
}

func TestRegistryInstalled(t *testing.T) {
	reg := NewRegistry(func(bin string) (string, error) {
		if bin == "codex" || bin == "claude" {
			return "/bin/" + bin, nil
		}
		return "", fmt.Errorf("not found")
	})
	got := reg.Installed()
	if len(got) != 2 {
		t.Fatalf("Installed = %v", got)
	}
}
