package clirun

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Banners seen across CLIs and releases all yield the same core version.
func TestFirstVersionTokenTolerance(t *testing.T) {
	for in, want := range map[string]string{
		"GitHub Copilot CLI 1.0.88.":                "1.0.88",
		"GitHub Copilot CLI 1.0.90-1.":              "1.0.90",
		"0.0.354\nCommit: 1a2b3c4":                  "0.0.354",
		"2.1.177 (Claude Code)":                     "2.1.177",
		"codex-cli 0.147.0":                         "0.147.0",
		"v1.2.3, built today":                       "1.2.3",
		"2026.09.26-dd393fe":                        "2026.09.26",
		"version: (1.18.25+build.7)":                "1.18.25",
		"Run 'copilot update' to check for updates": "",
	} {
		if got := firstVersionToken(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// A banner on stderr, colour codes, and a non-zero exit after printing
// the version (an update notice) still detect the CLI.
func TestCopilotVersionFromStderrWithColourAndNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "copilot", "#!/bin/sh\nprintf '\\033[1mGitHub Copilot CLI 1.0.90-1.\\033[0m\\n' >&2\nexit 1\n")
	v, err := Copilot.Version(context.Background(), path)
	if err != nil || v != "1.0.90" {
		t.Fatalf("version = %q, %v", v, err)
	}
}

// A binary that is found but cannot report a version is reported with
// its path and the reason, never as missing.
func TestDetectAllKeepsAFoundBinaryWhoseProbeFails(t *testing.T) {
	dir := t.TempDir()
	bad := writeStub(t, dir, "copilot", "#!/bin/sh\necho 'node: not found' >&2\nexit 127\n")
	reg := NewRegistry(func(bin string) (string, error) {
		if bin == "copilot" {
			return bad, nil
		}
		return "", errors.New("not found")
	})
	all := reg.DetectAll()
	p := all["copilot"]
	if p.Path != bad || p.Version != "" || p.Err == nil || !strings.Contains(p.Err.Error(), "node: not found") {
		t.Fatalf("copilot probe = %+v", p)
	}
	if c := all["claude"]; c.Path != "" || c.Err != nil {
		t.Fatalf("missing claude = %+v", c)
	}
	if reg.Detect()["copilot"] != "" {
		t.Fatal("Detect must keep \"\" for a CLI with no version")
	}
}
