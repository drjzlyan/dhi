package editor

import (
	"os"
	"strings"
	"testing"
	"time"
)

func renameMainEdit() []map[string]any {
	return []map[string]any{{
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 8},
			"end":   map[string]any{"line": 0, "character": 12}},
		"newText": "app",
	}}
}

func TestFormatOnSaveAppliesServerEdits(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.mu.Lock()
	srv.formatEdits = renameMainEdit()
	srv.mu.Unlock()
	path := m.active().Path()

	feed(m, ":")
	typeKeys(m, "w")
	feed(m, "enter")
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "package app") {
		t.Fatalf("saved file = %q, want formatted", got)
	}
	// One undo step restores the pre-format text in the buffer.
	feed(m, "u")
	if !strings.HasPrefix(m.active().Buffer().Text(), "package main") {
		t.Fatalf("undo = %q", m.active().Buffer().Text())
	}
}

func TestFormatFailureStillSavesAndSaysSo(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.mu.Lock()
	srv.formatFail = true
	srv.mu.Unlock()
	path := m.active().Path()
	feed(m, ":")
	typeKeys(m, "w")
	feed(m, "enter")
	msg := m.active().Message()
	if !strings.Contains(msg, "written") || !strings.Contains(msg, "saved unformatted") || !strings.Contains(msg, "syntax error") {
		t.Fatalf("message = %q", msg)
	}
	if got, _ := os.ReadFile(path); !strings.HasPrefix(string(got), "package main") {
		t.Fatalf("file must be saved as-is, got %q", got)
	}
}

func TestSetNoFmtSkipsFormatter(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.mu.Lock()
	srv.formatEdits = renameMainEdit()
	srv.mu.Unlock()
	feed(m, ":")
	typeKeys(m, "set nofmt")
	feed(m, "enter")
	if m.formatOnSave {
		t.Fatal(":set nofmt should disable format-on-save")
	}
	path := m.active().Path()
	feed(m, ":")
	typeKeys(m, "w")
	feed(m, "enter")
	if got, _ := os.ReadFile(path); !strings.HasPrefix(string(got), "package main") {
		t.Fatalf("nofmt must save unformatted, got %q", got)
	}
}

func TestFmtCommandFormatsNow(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.mu.Lock()
	srv.formatEdits = renameMainEdit()
	srv.mu.Unlock()
	feed(m, ":")
	typeKeys(m, "fmt")
	feed(m, "enter")
	if !strings.HasPrefix(m.active().Buffer().Text(), "package app") {
		t.Fatalf("buffer = %q", m.active().Buffer().Text())
	}
	if m.active().Buffer().Dirty() == false {
		t.Fatal(":fmt edits the buffer; it must stay dirty until :w")
	}
}

func TestFormatWithoutServerIsQuietOnSaveNamedOnCommand(t *testing.T) {
	m := newEditor(t) // no LSP manager
	feed(m, "enter", "down", "down", "enter")
	feed(m, ":")
	typeKeys(m, "fmt")
	feed(m, "enter")
	if got := m.active().Message(); !strings.Contains(got, "no language server") {
		t.Fatalf(":fmt message = %q", got)
	}
}

func TestWithTimeoutGivesUp(t *testing.T) {
	old := lspTimeout
	lspTimeout = 30 * time.Millisecond
	defer func() { lspTimeout = old }()
	_, err := withTimeout(func() (int, error) { time.Sleep(300 * time.Millisecond); return 1, nil })
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestSymbolPickerFilterAndJump(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "G", "i")
	typeKeys(m, "abcdefghij") // give line 1 enough columns to land on
	feed(m, "esc")
	feed(m, ":")
	typeKeys(m, "sym")
	feed(m, "enter")
	if m.mode != modeSymbols {
		t.Fatalf("mode = %v, msg=%q", m.mode, m.active().Message())
	}
	out := plainView(m)
	for _, want := range []string{"symbols", "func      main", "struct    Config", "method    Load"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker missing %q:\n%s", want, out)
		}
	}
	// Filtering narrows to the method; enter jumps to its line.
	typeKeys(m, "load")
	if got := m.syms.filtered(); len(got) != 1 || got[0].Name != "Load" {
		t.Fatalf("filtered = %+v", got)
	}
	feed(m, "enter")
	if m.mode != modeNav || m.syms != nil {
		t.Fatal("picker should close after a jump")
	}
	if cur := m.active().Buffer().Cursor(); cur.Line != 1 || cur.Col != 5 {
		t.Fatalf("cursor = %+v, want line 1 col 5", cur)
	}
}

func TestSymbolPickerEscAndRefusals(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, ":")
	typeKeys(m, "sym")
	feed(m, "enter")
	feed(m, "esc")
	if m.mode != modeNav || m.syms != nil {
		t.Fatal("esc must close the picker")
	}
	// No server → named refusal, no picker.
	n := newEditor(t)
	feed(n, "enter", "down", "down", "enter")
	feed(n, ":")
	typeKeys(n, "sym")
	feed(n, "enter")
	if n.mode == modeSymbols || !strings.Contains(n.active().Message(), "no language server") {
		t.Fatalf("msg = %q mode=%v", n.active().Message(), n.mode)
	}
}

// An agent edit (ApplyReplace) changes the buffer without telling the
// language server. formatEditor must resync first, or the server returns
// edit ranges computed against stale text and the save is garbled. The
// editor's key path usually resyncs incidentally (every keystroke runs
// lspSync, cached per *opened file* rather than per active tab), so this
// drives formatEditor directly to prove the guarantee does not depend on
// that accident.
func TestFormatResyncsAgentEditsBeforeAskingTheServer(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	ed := m.active()
	if err := m.ApplyReplace(ed.Path(), "package main", "package zeta", false); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	srv.events, srv.lastChange = nil, ""
	srv.formatEdits = nil
	srv.mu.Unlock()

	if note := m.formatEditor(ed); note != "" {
		t.Fatalf("formatEditor: %s", note)
	}
	srv.mu.Lock()
	events, seen := append([]string(nil), srv.events...), srv.lastChange
	srv.mu.Unlock()
	// didChange is a notification and formatting a request over one pipe, so
	// arrival order is send order.
	if len(events) < 2 || events[0] != "didChange" || events[len(events)-1] != "formatting" {
		t.Fatalf("events = %v, want didChange before formatting", events)
	}
	if !strings.HasPrefix(seen, "package zeta") {
		t.Fatalf("the server was formatting %q, not the agent-edited buffer", seen)
	}
}
