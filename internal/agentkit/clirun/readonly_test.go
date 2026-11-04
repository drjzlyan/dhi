package clirun

import (
	"slices"
	"strings"
	"testing"
)

// TestReadOnlyOKMatchesAMechanism keeps the declaration honest (F-144):
// an adapter that claims a read-only mode must change its argv, and one
// that does not must say why.
func TestReadOnlyOKMatchesAMechanism(t *testing.T) {
	for _, c := range NewRegistry(nil).All() {
		in := RunInput{Prompt: "p", Workdir: "/w", System: "s", MCPURL: "http://127.0.0.1:1/mcp"}
		on := in
		on.ReadOnly = true
		changes := !slices.Equal(c.BuildArgs(in), c.BuildArgs(on))
		if c.ReadOnlyOK != changes {
			t.Errorf("%s: ReadOnlyOK=%v but the argv mechanism exists=%v", c.Name, c.ReadOnlyOK, changes)
		}
		if !c.ReadOnlyOK && c.ReadOnlyWhy == "" {
			t.Errorf("%s has no read-only mode and does not say why", c.Name)
		}
	}
}

func TestClaudeReadOnlyDeniesWriteTools(t *testing.T) {
	c, _ := NewRegistry(nil).Get("claude")
	args := strings.Join(c.BuildArgs(RunInput{Prompt: "p", ReadOnly: true, DenyWeb: true}), " ")
	for _, want := range []string{"WebFetch", "Edit", "Write", "Bash"} {
		if !strings.Contains(args, want) {
			t.Errorf("claude read-only argv lacks %s: %s", want, args)
		}
	}
	if strings.Count(args, "--disallowedTools") != 1 {
		t.Errorf("want one --disallowedTools flag: %s", args)
	}
	if strings.Contains(strings.Join(c.BuildArgs(RunInput{Prompt: "p"}), " "), "disallowedTools") {
		t.Error("a normal turn must not deny tools")
	}
}

func TestCodexReadOnlyDropsTheBypass(t *testing.T) {
	c, _ := NewRegistry(nil).Get("codex")
	args := c.BuildArgs(RunInput{Prompt: "p", Workdir: "/w", ReadOnly: true})
	if !slices.Contains(args, "read-only") || slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("codex read-only argv = %v", args)
	}
	if !slices.Contains(c.BuildArgs(RunInput{Prompt: "p", Workdir: "/w"}), "--dangerously-bypass-approvals-and-sandbox") {
		t.Error("a normal turn keeps the bypass flag")
	}
}
