package clirun

import (
	"slices"
	"testing"
)

// F-179: claude takes plugin hooks per run through --settings; the flag is
// absent without a settings file, in both delivery shapes.
func TestClaudePerRunHooksSettings(t *testing.T) {
	for name, in := range map[string]RunInput{
		"argv":  {Prompt: "p", HooksConfig: "/tmp/dhi-hooks-1.json"},
		"stdin": {Stdin: "big", HooksConfig: "/tmp/dhi-hooks-1.json"},
	} {
		args := Claude.BuildArgs(in)
		i := slices.Index(args, "--settings")
		if i < 0 || args[i+1] != "/tmp/dhi-hooks-1.json" {
			t.Errorf("%s: args = %v", name, args)
		}
	}
	if slices.Contains(Claude.BuildArgs(RunInput{Prompt: "p"}), "--settings") {
		t.Error("--settings without hooks")
	}
	if Claude.HooksArgs == nil {
		t.Error("claude declares per-run hook input")
	}
}

// Every adapter without per-run hook input says why, so the doctor row is
// never an unexplained gap, and none passes the flag by accident.
func TestEveryAdapterWithoutHooksArgsExplainsTheGap(t *testing.T) {
	for _, c := range allAdapters() {
		if c.HooksArgs != nil {
			continue
		}
		if c.HooksWhy == "" {
			t.Errorf("%s has no per-run hook input and no HooksWhy", c.Name)
		}
		args := c.BuildArgs(RunInput{Prompt: "p", HooksConfig: "/tmp/x.json"})
		if slices.Contains(args, "/tmp/x.json") {
			t.Errorf("%s passed a hooks file it has no verified input for: %v", c.Name, args)
		}
	}
}
