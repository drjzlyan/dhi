package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLocateNeverConsultsThePath(t *testing.T) {
	shim, managed, other := t.TempDir(), t.TempDir(), t.TempDir()
	m := NewManager(shim, nil)
	m.SetManagedRoot(managed)

	touch(t, filepath.Join(other, "mine-ls"))
	t.Setenv("PATH", other) // a binary on the host PATH must stay invisible
	if _, ok := m.Locate("python", "mine-ls"); ok {
		t.Fatal("found a server on the host PATH")
	}
	if p, ok := m.Locate("python", filepath.Join(other, "mine-ls")); !ok || p != filepath.Join(other, "mine-ls") {
		t.Fatalf("an absolute command is the user's explicit choice: %q %v", p, ok)
	}
	if _, ok := m.Locate("python", filepath.Join(other, "missing")); ok {
		t.Fatal("a missing absolute command was accepted")
	}

	touch(t, filepath.Join(managed, "python", "node_modules", ".bin", "pyright-langserver"))
	if p, ok := m.Locate("python", "pyright-langserver"); !ok || p != filepath.Join(managed, "python", "node_modules", ".bin", "pyright-langserver") {
		t.Fatalf("managed install not found: %q %v", p, ok)
	}
	if _, ok := m.Locate("typescript", "pyright-langserver"); ok {
		t.Fatal("one language's managed install leaked into another")
	}

	touch(t, filepath.Join(shim, "gopls"))
	if p, ok := m.Locate("go", "gopls"); !ok || p != filepath.Join(shim, "gopls") {
		t.Fatalf("toolchain shim = %q %v", p, ok)
	}
	if got := m.ManagedPrefix("yaml"); got != filepath.Join(managed, "yaml") {
		t.Fatalf("prefix = %q", got)
	}
}

func TestCanFormatFollowsTheAdvertisedCapability(t *testing.T) {
	raw := func(s string) map[string]json.RawMessage {
		return map[string]json.RawMessage{"documentFormattingProvider": json.RawMessage(s)}
	}
	for _, c := range []struct {
		name string
		caps map[string]json.RawMessage
		want bool
	}{
		{"not reported (test double)", nil, true},
		{"true", raw("true"), true},
		{"options object", raw(`{"workDoneProgress":false}`), true},
		{"false", raw("false"), false},
		{"null", raw("null"), false},
		{"omitted (pyright)", map[string]json.RawMessage{"hoverProvider": json.RawMessage("true")}, false},
	} {
		if got := (&Client{caps: c.caps}).CanFormat(); got != c.want {
			t.Errorf("%s: CanFormat = %v, want %v", c.name, got, c.want)
		}
	}
}
