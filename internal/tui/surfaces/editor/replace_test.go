package editor

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// regexSearcher is the scripted searcher plus regex support, recording
// which entry point ran.
type regexSearcher struct {
	scriptedSearcher
	regexCalls int
}

func (s *regexSearcher) SearchRegex(ctx context.Context, p string, roots []string) (<-chan search.Hit, error) {
	s.regexCalls++
	return s.Search(ctx, p, roots)
}

func runSearch(m *Model, ss *regexSearcher, q string, regex bool) {
	m.HandleKey("s")
	if regex {
		m.HandleKey("ctrl+r")
	}
	for _, r := range q {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	for _, h := range ss.hits {
		m.Update(hitMsg(h))
	}
	m.Update(searchDoneMsg{})
}

// TestProjectReplaceFlow pins F-056: r previews, esc writes nothing,
// enter rewrites disk files and edits open buffers in place (undoable).
func TestProjectReplaceFlow(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	app := vpathAbs(t, ws, "alpha/app.go")
	main := vpathAbs(t, ws, "alpha/src/main.go")
	ss := &regexSearcher{scriptedSearcher: scriptedSearcher{hits: []search.Hit{
		{Path: app, Line: 1, Text: "package main"},
		{Path: main, Line: 1, Text: "package main"},
	}}}
	m := New("test", ws, WithSearcher(ss))
	m.Resize(120, 34)
	if m.OpenPaths(main) != 1 {
		t.Fatal("open main.go")
	}
	m.HandleKey("esc") // back to the tree, like a user would

	runSearch(m, ss, `package (\w+)`, true)
	if ss.regexCalls != 1 || !m.lastQueryRegex {
		t.Fatalf("regex mode did not reach the searcher (calls=%d)", ss.regexCalls)
	}
	m.HandleKey("r")
	for _, r := range "package ${1}_test" {
		m.HandleKey(string(r))
	}
	v := plainView(m)
	for _, want := range []string{"2 line(s) in 2 file(s)", "1 open buffer(s)", "+ package main_test"} {
		if !strings.Contains(v, want) {
			t.Fatalf("preview missing %q:\n%s", want, v)
		}
	}
	m.HandleKey("esc") // back out: nothing written
	if data, _ := os.ReadFile(app); string(data) != "package main\n" {
		t.Fatalf("esc wrote the file: %q", data)
	}

	m.HandleKey("r")
	for _, r := range "package ${1}_test" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	if data, _ := os.ReadFile(app); string(data) != "package main_test\n" {
		t.Fatalf("disk file = %q", data)
	}
	buf := m.bufferFor(main).ed.Buffer()
	if buf.Line(0) != "package main_test" || !buf.Dirty() {
		t.Fatalf("open buffer not edited in place: %q dirty=%v", buf.Line(0), buf.Dirty())
	}
	if data, _ := os.ReadFile(main); string(data) != "package main\n" {
		t.Fatal("an open buffer's file must not be written behind the user's back")
	}
	if !buf.Undo() || buf.Line(0) != "package main" {
		t.Fatal("the replace must be one undo step in the buffer")
	}
	if !strings.Contains(m.replaceNote, "replaced 2 line(s) in 2 file(s)") {
		t.Fatalf("note = %q", m.replaceNote)
	}
}
