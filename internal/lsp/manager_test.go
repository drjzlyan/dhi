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

func TestLocateUsesTheGivenLookup(t *testing.T) {
	other := t.TempDir()
	m := NewManager(func(name string) (string, bool) {
		if name == "gopls" {
			return "/opt/homebrew/bin/gopls", true
		}
		return "", false
	}, nil)
	if p, ok := m.Locate("gopls"); !ok || p != "/opt/homebrew/bin/gopls" {
		t.Fatalf("bare name = %q %v", p, ok)
	}
	if _, ok := m.Locate("pyright-langserver"); ok {
		t.Fatal("a missing server was found")
	}
	touch(t, filepath.Join(other, "mine-ls"))
	if p, ok := m.Locate(filepath.Join(other, "mine-ls")); !ok || p != filepath.Join(other, "mine-ls") {
		t.Fatalf("an absolute command is the user's explicit choice: %q %v", p, ok)
	}
	if _, ok := m.Locate(filepath.Join(other, "missing")); ok {
		t.Fatal("a missing absolute command was accepted")
	}
	if _, ok := NewManager(nil, nil).Locate("gopls"); ok {
		t.Fatal("no lookup: nothing is found")
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
