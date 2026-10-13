package runtime

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
)

func TestSanitizeReply(t *testing.T) {
	for in, want := range map[string]string{
		"Fixed the bug.":                         "Fixed the bug.",
		"tool: Read app/x.go\nFixed the bug.":    "Fixed the bug.",
		"command: go test ./...\nAll green now.": "All green now.",
		"Done.\n```sh\ncommand: keep me\n```":    "Done.\n```sh\ncommand: keep me\n```",
		"<function_calls>\nSummary: shipped.":    "Summary: shipped.",
		"tool: only echoes":                      "tool: only echoes", // never empties a reply
		"The tool: it works.":                    "The tool: it works.",
	} {
		if got := sanitizeReply(in); got != want {
			t.Errorf("sanitizeReply(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanFailureIsPlainLanguage(t *testing.T) {
	c := &clirun.CLI{Name: "claude", Bin: "claude"}
	for _, f := range []clirun.Failure{clirun.FailAuth, clirun.FailRateLimit, clirun.FailOverflow,
		clirun.FailTimeout, clirun.FailStall, clirun.FailNoFinal, clirun.FailCrash} {
		msg := humanFailure(f, c)
		if !strings.HasPrefix(msg, "I couldn't finish: ") || strings.Contains(msg, string(f)+")") {
			t.Errorf("%s: %q", f, msg)
		}
	}
	if !strings.Contains(humanFailure(clirun.FailAuth, c), "not authenticated") {
		t.Error("auth failure must say how to fix it")
	}
}

func TestAgentWorkingTracksPresence(t *testing.T) {
	r := &Runtime{}
	if r.AgentWorking("scout") {
		t.Fatal("idle agent reported working")
	}
	r.workStart(workKey("#general", 0))
	r.workStart(agentKey("scout"))
	if !r.AgentWorking("scout") || r.AgentWorking("lead") {
		t.Fatal("presence wrong")
	}
	if n := r.ActiveCount(); n != 1 {
		t.Fatalf("ActiveCount = %d, want 1 (presence keys are not turns)", n)
	}
	r.workDone(agentKey("scout"))
	if r.AgentWorking("scout") {
		t.Fatal("presence not cleared")
	}
}

func TestPresenceIsNilSafe(t *testing.T) {
	var r *Runtime // no crew installed: the app still wires the method values
	if r.AgentWorking("x") || r.Working("#general", 0) || r.ActiveCount() != 0 {
		t.Fatal("nil runtime reported work")
	}
}
