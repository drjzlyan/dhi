package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/lsp"
)

// runQueued executes the work keys queued and feeds the results back.
func runQueued(m *Model) {
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, cc := range msg {
				run(cc)
			}
		case nil:
		default:
			m.Update(msg)
		}
	}
	run(m.TakeCmd())
}

func fileURI(p string) string { return "file://" + p }

// F-116: gd jumps to the one definition; gR lists references.
func TestGoToDefinitionAndReferences(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	helper := filepath.Join(m.ws.Root, "repos", "alpha", "src", "util", "helper.go")
	srv.mu.Lock()
	srv.locs = []map[string]any{{"uri": fileURI(helper), "range": map[string]any{
		"start": map[string]any{"line": 0, "character": 8}, "end": map[string]any{"line": 0, "character": 12}}}}
	srv.mu.Unlock()
	feed(m, "g", "d")
	runQueued(m)
	if e := m.active(); e.Path() != helper || e.Buffer().Cursor().Col != 8 {
		t.Fatalf("gd landed in %s at %+v", e.Path(), e.Buffer().Cursor())
	}
	srv.mu.Lock()
	srv.locs = append(srv.locs, srv.locs[0])
	srv.mu.Unlock()
	feed(m, "g", "R")
	runQueued(m)
	if m.mode != modeResults || len(m.hits) != 2 || !strings.Contains(plainView(m), "references to util") {
		t.Fatalf("refs list: mode=%v hits=%d\n%s", m.mode, len(m.hits), plainView(m))
	}
	m.HandleKey("r") // a reference list has nothing to replace
	if m.replace != nil {
		t.Fatal("replace opened on a reference list")
	}
}

// F-116: accepting a completion replaces the typed prefix.
func TestCompletionReplacesTypedPrefix(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "o")
	typeKeys(m, "Pri")
	feed(m, "ctrl+space")
	waitFor(t, m, func() bool { return m.compOpen }, "completion popup")
	feed(m, "enter")
	e := m.active()
	if got := e.Buffer().Line(e.Buffer().Cursor().Line); got != "Println" {
		t.Fatalf("line = %q, want Println", got)
	}
}

// F-116: a rename edits files that are not open, on disk.
func TestRenameEditsUnopenedFile(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	helper := filepath.Join(m.ws.Root, "repos", "alpha", "src", "util", "helper.go")
	srv.mu.Lock()
	srv.renameExtra = fileURI(helper)
	srv.mu.Unlock()
	feed(m, "g", "r")
	typeKeys(m, "zeta")
	feed(m, "enter")
	waitFor(t, m, func() bool {
		b, _ := os.ReadFile(helper)
		return string(b) == "package zeta\n"
	}, "unopened file renamed on disk")
}

// F-116: the cursor line's problem shows, and ]d jumps between problems.
func TestDiagnosticMessageAndJump(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	if !strings.Contains(plainView(m), "boom") {
		t.Fatalf("diagnostic message not shown:\n%s", plainView(m))
	}
	path := m.active().Path()
	m.lspDiags[path] = append(m.lspDiags[path], lsp.Diagnostic{Path: path, Line: 0, Col: 3, Severity: 2, Message: "later"})
	feed(m, "]", "d")
	if c := m.active().Buffer().Cursor(); c.Line != 0 {
		t.Fatalf("]d cursor %+v", c)
	}
	feed(m, ":", "d", "i", "a", "g", "enter")
	if m.mode != modeResults || len(m.hits) != 2 {
		t.Fatalf(":diag: mode=%v hits=%d", m.mode, len(m.hits))
	}
}
