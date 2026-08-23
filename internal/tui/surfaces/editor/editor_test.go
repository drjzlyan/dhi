package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func setupWorkspace(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	beta := filepath.Join(root, "other", "beta")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(alpha, "app.go"), "package main\n")
	write(filepath.Join(alpha, "src", "main.go"), "package main\n")
	write(filepath.Join(alpha, "src", "util", "helper.go"), "package util\n")
	write(filepath.Join(beta, "README.md"), "# beta\n")

	cfg := "schema = 1\n\n[members.alpha]\npath = \"repos/alpha\"\n\n[members.beta]\npath = \"" + beta + "\"\n"
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dhi", "workspace.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws, root
}

func newEditor(t *testing.T) *Model {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	m := New("test", ws)
	m.Resize(100, 30)
	return m
}

func plainView(m *Model) string { return ansi.Strip(m.View()) }

// feed routes each keystroke through the surface.
func feed(m *Model, keys ...string) {
	for _, k := range keys {
		m.HandleKey(k)
	}
}

// typeKeys feeds each rune of s as its own keystroke.
func typeKeys(m *Model, s string) {
	for _, r := range s {
		m.HandleKey(string(r))
	}
}

func TestEmptyStateWithoutWorkspace(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", nil)
	m.Resize(80, 24)
	if !strings.Contains(plainView(m), "no workspace loaded") {
		t.Errorf("empty state missing:\n%s", m.View())
	}
}

func TestTreeGroupsByMemberAndExpands(t *testing.T) {
	m := newEditor(t)
	v := plainView(m)
	if !strings.Contains(v, "alpha/") || !strings.Contains(v, "beta/") {
		t.Fatalf("member headers missing:\n%s", v)
	}
	if strings.Contains(v, "main.go") {
		t.Fatal("collapsed tree leaked files")
	}

	m.HandleKey("enter") // expand alpha/
	v = plainView(m)
	if !strings.Contains(v, "app.go") || !strings.Contains(v, "src/") {
		t.Fatalf("alpha contents missing after expand:\n%s", v)
	}

	// Dirs sort first: rows are alpha, src/, app.go.
	m.HandleKey("down") // -> src/
	if n := m.rows[m.list.Cursor].node; n.name != "src" {
		t.Fatalf("cursor on %q, want src", n.name)
	}
	m.HandleKey("enter") // expand src/ → util/, main.go
	if !strings.Contains(plainView(m), "main.go") || !strings.Contains(plainView(m), "util/") {
		t.Fatalf("src/ contents missing:\n%s", plainView(m))
	}

	m.HandleKey("down") // -> util/
	m.HandleKey("down") // -> main.go
	if n := m.rows[m.list.Cursor].node; n.name != "main.go" {
		t.Fatalf("cursor on %q, want main.go", n.name)
	}
	m.HandleKey("enter") // open
	if m.openVPath != "alpha/src/main.go" {
		t.Errorf("openVPath = %q", m.openVPath)
	}
	v = plainView(m)
	if !strings.Contains(v, "package main") || !strings.Contains(v, "NORMAL") {
		t.Errorf("buffer not rendered:\n%s", v)
	}
}

func TestCollapseWithH(t *testing.T) {
	m := newEditor(t)
	m.HandleKey("enter") // expand alpha
	if !strings.Contains(plainView(m), "app.go") {
		t.Fatal("expand failed")
	}
	m.HandleKey("h") // collapse alpha
	if strings.Contains(plainView(m), "app.go") {
		t.Error("collapse failed")
	}
}

func TestFuzzyFindOpensAndReveals(t *testing.T) {
	m := newEditor(t)

	if !m.HandleKey("/") {
		t.Fatal("/ did not open finder")
	}
	if !strings.Contains(plainView(m), "find file") {
		t.Fatalf("finder overlay missing:\n%s", m.View())
	}

	for _, r := range "hlpr" {
		m.HandleKey(string(r))
	}
	v := plainView(m)
	if !strings.Contains(v, "alpha/src/util/helper.go") {
		t.Fatalf("query results missing helper.go:\n%s", m.View())
	}

	m.HandleKey("enter")
	if m.mode != modeNav {
		t.Fatal("enter did not return to nav mode")
	}
	if m.openVPath != "alpha/src/util/helper.go" {
		t.Errorf("openVPath = %q", m.openVPath)
	}
	v = plainView(m)
	for _, want := range []string{"alpha/", "util/", "helper.go"} {
		if !strings.Contains(v, want) {
			t.Errorf("tree did not reveal %q:\n%s", want, v)
		}
	}
	if cur := m.rows[m.list.Cursor].node; cur == nil || cur.name != "helper.go" {
		t.Errorf("cursor on %v, want helper.go", cur)
	}
}

func TestFinderEscCancels(t *testing.T) {
	m := newEditor(t)
	m.HandleKey("/")
	for _, r := range "x" {
		m.HandleKey(string(r))
	}
	m.HandleKey("esc")
	if m.mode != modeNav {
		t.Fatal("esc did not cancel finder")
	}
	m.HandleKey("/") // reopen: query starts fresh
	if len(m.query) != 0 {
		t.Errorf("reopened finder kept old query %q", string(m.query))
	}
}

func TestEditorGolden(t *testing.T) {
	m := newEditor(t)
	golden.Snapshot(t, "editor_tree_collapsed_100x30", m.View())

	m.HandleKey("enter") // expand alpha
	m.HandleKey("down")  // src/
	m.HandleKey("enter") // expand src
	m.HandleKey("down")
	m.HandleKey("down")  // main.go
	m.HandleKey("enter") // open into a buffer
	golden.Snapshot(t, "editor_open_file_100x30", m.View())

	// insert-session editing with mode chip + dirty marker
	feed(m, "$")
	feed(m, "i")
	typeKeys(m, "  // entrypoint")
	feed(m, "esc")
	golden.Snapshot(t, "editor_buffer_edited_100x30", m.View())
}

func TestEditorSearchGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	abs := func(vp string) string {
		p, err := workspace.ParseVPath(vp)
		if err != nil {
			t.Fatal(err)
		}
		a, err := ws.Resolve(p)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	ss := &scriptedSearcher{hits: []search.Hit{
		{Path: abs("alpha/src/main.go"), Line: 3, Column: 0, Text: "func main() {"},
		{Path: abs("alpha/app.go"), Line: 7, Column: 2, Text: "// main entrypoint"},
	}}
	m := New("test", ws, WithSearcher(ss))
	m.Resize(100, 30)

	m.HandleKey("s")
	for _, r := range "main" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	for _, h := range ss.hits {
		m.Update(hitMsg(h))
	}
	m.Update(searchDoneMsg{})
	golden.Snapshot(t, "editor_search_results_100x30", m.View())
}

func TestHandleKeyNoWorkspaceIsNoop(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("test", nil)
	m.Resize(60, 20)
	if m.HandleKey("j") || m.HandleKey("/") || m.HandleKey("enter") {
		t.Error("keys consumed without a workspace")
	}
}

func TestBufferEditAndSaveRoundTrip(t *testing.T) {
	ws, root := setupWorkspace(t)
	m := New("test", ws)
	m.Resize(100, 30)
	target := filepath.Join(root, "repos", "alpha", "src", "main.go")

	// open via tree: expand alpha → src → open main.go
	feed(m, "enter") // alpha
	feed(m, "down", "enter")
	feed(m, "down", "down")
	feed(m, "enter")
	if m.active() == nil || !m.bufFocus {
		t.Fatal("buffer not focused after open")
	}

	feed(m, "$")
	feed(m, "i")
	typeKeys(m, "// hi")
	feed(m, "esc")
	if !m.active().Buffer().Dirty() {
		t.Fatal("edit did not dirty the buffer")
	}

	feed(m, ":")
	if m.active().Mode() != textbuf.ModeCommand {
		t.Fatal(": did not enter command mode")
	}
	typeKeys(m, "wq")
	feed(m, "enter")

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "// hi") {
		t.Fatalf("save missing edit: %q", string(data))
	}
	if m.active() != nil {
		t.Error(":wq did not close the buffer")
	}
	if m.bufFocus {
		t.Error("focus not returned to tree")
	}
}

func TestEscReturnsFocusToTree(t *testing.T) {
	m := newEditor(t)
	feed(m, "enter") // expand alpha (cursor stays on repo)
	feed(m, "down")  // src/
	feed(m, "down")  // app.go
	feed(m, "enter") // open
	if !m.bufFocus {
		t.Fatal("expected buffer focus")
	}
	// esc in normal mode → tree; second esc does nothing harmful
	feed(m, "esc")
	if m.bufFocus {
		t.Fatal("esc did not return focus to tree")
	}
	if !m.HandleKey("j") {
		t.Error("tree keys dead after refocus")
	}
}

func TestQuitDirtyRefuses(t *testing.T) {
	m := newEditor(t)
	feed(m, "enter", "down", "down", "enter") // open app.go
	feed(m, "i")
	typeKeys(m, "x")
	feed(m, "esc")
	feed(m, ":")
	typeKeys(m, "q")
	feed(m, "enter")
	if m.active() == nil {
		t.Fatal("dirty :q closed the buffer")
	}
	if !strings.Contains(plainView(m), "no write") {
		t.Errorf("refusal message missing:\n%s", plainView(m))
	}
}

func TestMultiBuffersAndExSwitching(t *testing.T) {
	ws, _ := setupWorkspace(t)
	m := New("test", ws)
	m.Resize(100, 30)

	// open alpha/src/main.go
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	// open app.go via finder for speed
	feed(m, "esc") // tree focus
	feed(m, "/")
	typeKeys(m, "app.go")
	feed(m, "enter")

	if len(m.bufs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(m.bufs))
	}
	if m.bufs[m.activeTab].vp != "alpha/app.go" {
		t.Fatalf("active = %q", m.bufs[m.activeTab].vp)
	}

	// :bn wraps to first buffer
	feed(m, ":")
	typeKeys(m, "bn")
	feed(m, "enter")
	if m.bufs[m.activeTab].vp != "alpha/src/main.go" {
		t.Fatalf("after :bn active = %q", m.bufs[m.activeTab].vp)
	}
	if !strings.Contains(plainView(m), "[main.go]") {
		t.Errorf("tab strip missing active marker:\n%s", plainView(m))
	}

	// :bp back
	feed(m, ":")
	typeKeys(m, "bp")
	feed(m, "enter")
	if m.bufs[m.activeTab].vp != "alpha/app.go" {
		t.Fatalf("after :bp active = %q", m.bufs[m.activeTab].vp)
	}
}

func TestBufferByPatternAndAmbiguity(t *testing.T) {
	ws, _ := setupWorkspace(t)
	m := New("test", ws)
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	feed(m, "esc")
	feed(m, "/")
	typeKeys(m, "app.go")
	feed(m, "enter")

	feed(m, ":")
	typeKeys(m, "b src/main.go")
	feed(m, "enter")
	if m.bufs[m.activeTab].vp != "alpha/src/main.go" {
		t.Fatalf(":b pattern → %q", m.bufs[m.activeTab].vp)
	}

	feed(m, ":")
	typeKeys(m, "b a")
	feed(m, "enter")
	if !strings.Contains(plainView(m), "more than one match") {
		t.Errorf("ambiguity not reported:\n%s", plainView(m))
	}
}

func TestOpenReusesExistingBuffer(t *testing.T) {
	m := newEditor(t)
	feed(m, "enter", "down", "down", "enter") // open app.go
	first := len(m.bufs)
	if first != 1 {
		t.Fatalf("tabs = %d", first)
	}
	feed(m, "esc") // tree
	// reopen the same file from the tree cursor position
	feed(m, "enter")
	if len(m.bufs) != first {
		t.Errorf("reopen duplicated buffer: %d tabs", len(m.bufs))
	}
	if !m.bufFocus || m.active() == nil {
		t.Error("reopen did not focus existing buffer")
	}
}

func TestCloseMiddleActivatesNeighbor(t *testing.T) {
	ws, _ := setupWorkspace(t)
	m := New("test", ws)
	m.Resize(100, 30)

	// three distinct files
	for _, q := range []string{"app.go", "main.go", "helper.go"} {
		feed(m, "esc")
		feed(m, "/")
		typeKeys(m, q)
		feed(m, "enter")
	}
	if len(m.bufs) != 3 {
		t.Fatalf("tabs = %d", len(m.bufs))
	}

	// close middle tab (helper.go was last opened; switch to middle first)
	feed(m, ":")
	typeKeys(m, "b main.go")
	feed(m, "enter")
	feed(m, ":")
	typeKeys(m, "q")
	feed(m, "enter")

	if len(m.bufs) != 2 {
		t.Fatalf("after close tabs = %d", len(m.bufs))
	}
	if vp := m.bufs[m.activeTab].vp; vp != "beta/README.md" && vp != "alpha/src/util/helper.go" && vp != "alpha/app.go" {
		t.Errorf("neighbor = %q", vp)
	}
	if m.activeTab >= len(m.bufs) {
		t.Error("active index out of range after close")
	}
}
