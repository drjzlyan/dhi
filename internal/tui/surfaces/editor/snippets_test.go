package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/lsp"
)

func lineText(m *Model) string {
	e := m.active()
	return e.Buffer().Line(e.Buffer().Cursor().Line)
}

// F-171: a snippet item from the server expands, tabs between stops and
// ends at the last one.
func TestLSPSnippetCompletionExpandsAndTabs(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.mu.Lock()
	srv.compItems = []map[string]any{{
		"label": "Printf", "insertTextFormat": 2,
		"insertText": "Printf(${1:format}, ${2:args})$0",
	}}
	caps := srv.initCaps
	srv.mu.Unlock()
	if !strings.Contains(caps, `"snippetSupport":true`) {
		t.Fatalf("client must advertise snippetSupport: %s", caps)
	}
	feed(m, "o")
	typeKeys(m, "Pri")
	feed(m, "ctrl+space")
	waitFor(t, m, func() bool { return m.compOpen }, "completion popup")
	feed(m, "enter")
	if lineText(m) != "Printf(format, args)" || m.snip == nil {
		t.Fatalf("line %q session %v", lineText(m), m.snip)
	}
	feed(m, "x") // typing replaces the selected placeholder
	feed(m, "tab")
	typeKeys(m, "y")
	if lineText(m) != "Printf(x, y)" {
		t.Fatalf("line %q", lineText(m))
	}
	feed(m, "tab")
	if m.snip != nil {
		t.Fatal("tab onto $0 must end the session")
	}
	feed(m, "z")
	if lineText(m) != "Printf(x, y)z" {
		t.Fatalf("typing after $0: %q", lineText(m))
	}
}

// F-171: tab precedence in insert mode: popup accept, then snippet jump,
// then indent.
func TestSnippetTabPrecedence(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.mu.Lock()
	srv.compItems = []map[string]any{{"label": "f", "insertTextFormat": 2, "insertText": "f(${1:a}, ${2:b})"}}
	srv.mu.Unlock()
	feed(m, "o")
	typeKeys(m, "f")
	feed(m, "ctrl+space")
	waitFor(t, m, func() bool { return m.compOpen }, "completion popup")
	feed(m, "enter")
	if m.snip == nil {
		t.Fatal("no session")
	}
	// 1. an open popup wins over the jump
	m.compItems = []lsp.CompletionItem{{Label: "zz", Insert: "zz"}}
	m.compOpen = true
	feed(m, "tab")
	if m.compOpen || lineText(m) != "f(zz, b)" || m.snip == nil || m.snip.Progress() != "1/2" {
		t.Fatalf("popup tab: line %q", lineText(m))
	}
	// 2. no popup: the jump
	feed(m, "tab")
	typeKeys(m, "q")
	if lineText(m) != "f(zz, q)" {
		t.Fatalf("jump: %q", lineText(m))
	}
	// 3. no session: tab indents
	feed(m, "esc", "o", "tab")
	if m.snip != nil || !strings.Contains(lineText(m), "\t") {
		t.Fatalf("indent: %q session %v", lineText(m), m.snip)
	}
}

func writeSnippets(t *testing.T, m *Model, body string) {
	t.Helper()
	dir := filepath.Join(m.ws.Root, "repos", "alpha", ".vscode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.code-snippets"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const userSnips = `{
	// comments are fine
	"Error check": {
		"prefix": "iferr",
		"body": ["if ${1:err} != nil {", "\treturn ${1:err}", "}", "$0"],
		"description": "return the error",
		"scope": "go",
	},
	"Rust only": {"prefix": "iferrrs", "body": "x", "scope": "rust"},
}`

// F-171: a member's .vscode/*.code-snippets are offered by prefix, honour
// scope, expand with mirrors, and the continuation lines keep the indent.
func TestUserSnippetsFromVSCodeFile(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	writeSnippets(t, m, userSnips)
	feed(m, "o", "tab")
	typeKeys(m, "ife")
	feed(m, "ctrl+space")
	waitFor(t, m, func() bool { return m.compOpen }, "completion popup")
	var labels []string
	for _, it := range m.compItems {
		labels = append(labels, it.Label)
	}
	if len(labels) == 0 || labels[0] != "iferr" || strings.Contains(strings.Join(labels, ","), "iferrrs") {
		t.Fatalf("labels %v (scope go only)", labels)
	}
	feed(m, "enter")
	e := m.active()
	cur := e.Buffer().Cursor()
	if got := e.Buffer().Line(cur.Line); got != "\tif err != nil {" {
		t.Fatalf("line %q", got)
	}
	typeKeys(m, "e2") // replaces "err" in both places
	if got := e.Buffer().Line(cur.Line + 1); got != "\t\treturn e2" {
		t.Fatalf("mirror/indent: %q", got)
	}
	if got := e.Buffer().Line(cur.Line); got != "\tif e2 != nil {" {
		t.Fatalf("first: %q", got)
	}
	feed(m, "tab") // only $0 remains
	if m.snip != nil {
		t.Fatal("session should have ended at $0")
	}
	if c := e.Buffer().Cursor(); c.Line != cur.Line+3 {
		t.Fatalf("cursor %+v", c)
	}
}

// F-171: snippets complete in a file with no language server, and a
// broken snippet file is named on the status line.
func TestUserSnippetsWithoutServerAndBadFile(t *testing.T) {
	ws, _ := setupWorkspace(t)
	m := New("test", ws)
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	writeSnippets(t, m, `{oops`)
	feed(m, "o")
	typeKeys(m, "a")
	feed(m, "ctrl+space")
	if m.compOpen || !strings.Contains(m.active().Message(), "go.code-snippets") {
		t.Fatalf("open=%v message=%q", m.compOpen, m.active().Message())
	}
	writeSnippets(t, m, userSnips)
	feed(m, "ctrl+u")
	typeKeys(m, "iferr")
	feed(m, "ctrl+space")
	waitFor(t, m, func() bool { return m.compOpen }, "snippet popup without a server")
	feed(m, "tab")
	if m.snip == nil {
		t.Fatal("expected a snippet session")
	}
}
