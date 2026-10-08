package clirun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssess(t *testing.T) {
	cases := []struct {
		tested, found string
		want          Verdict
		say           string
	}{
		{"2.1.177", "2.1.177", VerdictExact, "verified"},
		{"2.1.177", "2.1.285", VerdictDrift, "newer than the verified"},
		{"2.1.177", "2.0.9", VerdictDrift, "older than the verified"},
		{"0.147.0", "0.148.2", VerdictDrift, "newer"},
		{"2.1.177", "3.0.0", VerdictMajor, "different major"},
		{"1.18.25", "v1.19.0", VerdictDrift, "newer"},
		{"2026.09.26", "2026.10.02", VerdictDrift, "newer"},
		{"2026.09.26", "2027.01.03", VerdictMajor, "different major"},
		{"1.2.11", "1.2.11-beta.2", VerdictDrift, "a variant of the verified"},
		{"1.2.11", "banana", VerdictUnknown, "not comparable"},
		{"banana", "1.2.3", VerdictUnknown, "not comparable"},
	}
	for _, c := range cases {
		got, msg := Assess(c.tested, c.found)
		if got != c.want || !strings.Contains(msg, c.say) {
			t.Errorf("Assess(%q,%q) = %v %q; want %v containing %q", c.tested, c.found, got, msg, c.want, c.say)
		}
	}
}

func TestEveryRegisteredCLIHasAnInstallPlan(t *testing.T) {
	for _, name := range CLINames() {
		p, ok := PlanFor(name)
		if !ok {
			t.Errorf("%s has no install plan", name)
			continue
		}
		if p.Auth == "" && name != "antigravity" {
			t.Errorf("%s: no sign-in hint", name)
		}
		switch p.Method {
		case MethodNPM:
			if p.Package == "" || p.Command != "" {
				t.Errorf("%s: npm plan needs a package and no shell command: %+v", name, p)
			}
		case MethodManual:
			if p.Package != "" {
				t.Errorf("%s: manual plan must not name a package DHI would install: %+v", name, p)
			}
			if p.Command == "" && p.Note == "" {
				t.Errorf("%s: manual plan with no command must say why", name)
			}
		default:
			t.Errorf("%s: unknown method %q", name, p.Method)
		}
	}
}

func TestManagedLookPathWinsThenManagedDir(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(ManagedBinDir(root, "codex"), "codex")
	os.MkdirAll(filepath.Dir(managed), 0o755)
	os.WriteFile(managed, []byte("#!/bin/sh\n"), 0o755)

	notFound := func(string) (string, error) { return "", os.ErrNotExist }
	onPath := func(b string) (string, error) { return "/usr/bin/" + b, nil }

	if p, err := ManagedLook(root, notFound)("codex"); err != nil || p != managed {
		t.Fatalf("managed = %q, %v", p, err)
	}
	if p, _ := ManagedLook(root, onPath)("codex"); p != "/usr/bin/codex" {
		t.Fatalf("the user's own install must win, got %q", p)
	}
	if _, err := ManagedLook(root, notFound)("claude"); err == nil {
		t.Fatal("a CLI that is nowhere must stay not found")
	}
	// A non-executable file is not a CLI.
	os.WriteFile(filepath.Join(ManagedBinDir(root, "codex"), "claude"), []byte("x"), 0o644)
	if _, err := ManagedLook(root, notFound)("claude"); err == nil {
		t.Fatal("a non-executable file was accepted")
	}
	if _, err := ManagedLook("", notFound)("codex"); err == nil {
		t.Fatal("no root, no managed lookup")
	}
}

func TestRelation(t *testing.T) {
	cases := map[[2]string]string{
		{"2.1.177", "2.1.285"}: "newer than", {"2.1.177", "2.0.9"}: "older than",
		{"1.2.11", "1.2.11-rc1"}: "a variant of", {"2.1.177", "2.1.177"}: "",
		{"2.1.177", "3.0.0"}: "", {"x", "y"}: "",
	}
	for in, want := range cases {
		if got := Relation(in[0], in[1]); got != want {
			t.Errorf("Relation(%q,%q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
