package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Live LSP (F-163): drives the editor's real code paths against a real
// gopls on the user's PATH. Opt in with DHI_LIVE_LSP=1:
//
//	DHI_LIVE_LSP=1 go test -run Live -v ./internal/tui/surfaces/editor
//
// The editor starts gopls itself (the Manager's own PATH lookup, as in
// production), so this also covers the lazy server start (F-116).

type liveHarness struct {
	t    *testing.T
	m    *Model
	root string // the module (and workspace member) directory
	msgs chan tea.Msg
}

func newLiveHarness(t *testing.T) *liveHarness {
	t.Helper()
	if os.Getenv("DHI_LIVE_LSP") == "" {
		t.Skip("set DHI_LIVE_LSP=1 to run against a real gopls")
	}
	bin, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls is not on PATH")
	}
	if out, err := exec.Command(bin, "version").CombinedOutput(); err == nil {
		t.Logf("gopls: %s", strings.Fields(strings.TrimSpace(string(out)))[len(strings.Fields(strings.TrimSpace(string(out))))-1])
	}

	wsRoot := t.TempDir()
	if r, err := filepath.EvalSymlinks(wsRoot); err == nil {
		wsRoot = r // macOS /var -> /private/var: gopls reports real paths
	}
	mod := filepath.Join(wsRoot, "mod")
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(mod, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/live\n\ngo 1.21\n")
	write("a.go", "package live\n\n// Add sums two ints.\nfunc Add(a int, b int) int { return a + b }\n")
	write("b.go", "package live\n\nfunc Use() int {\n\treturn Add(1, 2)\n}\n")
	write("h.go", "package live\n\nfunc hinted() int {\n\tv := Add(1, 2)\n\treturn v\n}\n")
	write("d.go", "package live\n\nfunc scratch() {\n\t\n}\n")
	write("sub/e.go", "package sub\n\nvar _ = strings.ToUpper(\"a\")\n")
	write("sub/c.go", "package sub\n\nfunc broken() {\n\tx := 1\n}\n")
	if err := os.MkdirAll(filepath.Join(wsRoot, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema = 1\n\n[members.mod]\npath = \"mod\"\n"
	if err := os.WriteFile(filepath.Join(wsRoot, ".dhi", "workspace.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	mgr := lsp.NewManager(func(name string) (string, bool) {
		p, err := exec.LookPath(name)
		return p, err == nil
	}, os.Environ)
	t.Cleanup(func() { mgr.ShutdownAll() })

	// No trust gate is configured (New without WithTrust trusts everything),
	// so restricted mode (F-148) does not stand in front of the server.
	m := New("test", ws, WithLSP(mgr))
	m.Resize(120, 40)
	return &liveHarness{t: t, m: m, root: mod, msgs: make(chan tea.Msg, 256)}
}

// run executes a command in the background and feeds its message back to the
// model from wait, as the bubbletea runtime would.
func (h *liveHarness) run(c tea.Cmd) {
	if c == nil {
		return
	}
	go func() {
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, cc := range msg {
				h.run(cc)
			}
		case nil:
		default:
			h.msgs <- msg
		}
	}()
}

// pump applies finished work and starts newly queued work.
func (h *liveHarness) pump() {
	h.run(h.m.TakeCmd())
	for {
		select {
		case msg := <-h.msgs:
			h.run(h.m.Update(msg))
		default:
			h.m.drainTerm()
			h.run(h.m.TakeCmd())
			return
		}
	}
}

func (h *liveHarness) wait(cond func() bool, what string) {
	h.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		h.pump()
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	h.t.Fatalf("timeout waiting for %s:\n%s", what, plainView(h.m))
}

func (h *liveHarness) path(rel string) string { return filepath.Join(h.root, rel) }

func (h *liveHarness) open(rel string) {
	h.t.Helper()
	if n := h.m.OpenPaths(h.path(rel)); n != 1 {
		h.t.Fatalf("open %s: %d", rel, n)
	}
	h.pump()
	if got := h.m.active().Path(); got != h.path(rel) {
		h.t.Fatalf("active = %s, want %s", got, h.path(rel))
	}
}

func (h *liveHarness) at(line, col int) {
	h.m.active().Buffer().SetCursor(textbuf.Pos{Line: line, Col: col})
}

func (h *liveHarness) keys(k ...string) { feed(h.m, k...); h.pump() }

func (h *liveHarness) readDisk(rel string) string {
	b, err := os.ReadFile(h.path(rel))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

// diagCount is how many problems the server reported for rel.
func (h *liveHarness) diagCount(rel string) int { return len(h.m.lspDiags[h.path(rel)]) }

func TestLiveGopls(t *testing.T) {
	h := newLiveHarness(t)
	m := h.m

	// Diagnostics arrive for a file with an unused variable (and the lazy
	// start of gopls works).
	t.Run("diagnostics", func(t *testing.T) {
		h.open("sub/c.go")
		h.wait(func() bool { return h.diagCount("sub/c.go") > 0 }, "diagnostics for sub/c.go")
		d := m.lspDiags[h.path("sub/c.go")][0]
		t.Logf("diagnostic: line %d %q", d.Line, d.Message)
		if d.Line != 3 || !strings.Contains(d.Message, "x") {
			t.Fatalf("unexpected diagnostic %+v", d)
		}
	})

	// F-158 is absent on this base; the older `ga` flow is covered here:
	// the quick fix for the unused variable.
	t.Run("code actions", func(t *testing.T) {
		h.open("sub/e.go")
		h.wait(func() bool { return h.diagCount("sub/e.go") > 0 }, "diagnostics for sub/e.go")
		h.at(2, 9)
		h.keys("g", "a")
		h.wait(func() bool { return m.actionOpen }, "code action popup")
		var titles []string
		pick := -1
		for i, a := range m.actionItems {
			titles = append(titles, a.Title)
			if pick < 0 && strings.Contains(a.Title, "Add import") {
				pick = i
			}
		}
		t.Logf("actions: %q", titles)
		if pick < 0 {
			t.Fatalf("no import quick fix among %q", titles)
		}
		for i := 0; i < pick; i++ {
			h.keys("down")
		}
		h.keys("enter")
		h.wait(func() bool { return strings.Contains(m.active().Buffer().Text(), `import "strings"`) }, "import added")
	})

	// A symbol used across files: b.go calls Add, declared in a.go.
	t.Run("hover", func(t *testing.T) {
		h.open("b.go")
		h.at(3, 9)
		h.keys("K")
		h.wait(func() bool { return m.hoverOpen }, "hover popup")
		v := plainView(m)
		if !strings.Contains(v, "Add") || !strings.Contains(v, "sums two ints") {
			t.Fatalf("hover lacks signature/doc:\n%s", v)
		}
		h.keys("j")
	})

	t.Run("gd across files", func(t *testing.T) {
		h.open("b.go")
		h.at(3, 9)
		h.keys("g", "d")
		h.wait(func() bool { return m.active().Path() == h.path("a.go") }, "jump to a.go")
		if c := m.active().Buffer().Cursor(); c.Line != 3 || c.Col != 5 {
			t.Fatalf("gd cursor %+v, want line 3 col 5", c)
		}
	})

	t.Run("gR references", func(t *testing.T) {
		h.open("a.go")
		h.at(3, 5)
		h.keys("g", "R")
		h.wait(func() bool { return m.mode == modeResults && len(m.hits) >= 2 }, "reference list")
		var files []string
		for _, hit := range m.hits {
			files = append(files, filepath.Base(hit.hit.Path))
		}
		t.Logf("references in %v", files)
		seen := map[string]bool{}
		for _, f := range files {
			seen[f] = true
		}
		if !seen["a.go"] || !seen["b.go"] {
			t.Fatalf("references %v lack a.go or b.go", files)
		}
		h.keys("esc")
	})

	t.Run("workspace symbols", func(t *testing.T) {
		h.keys(":", "w", "s", "y", "m", " ", "A", "d", "d", "enter")
		h.wait(func() bool {
			return m.wsym != nil && len(m.wsym.rows) > 0
		}, "workspace symbol rows")
		found := false
		for _, r := range m.wsym.rows {
			if r.sym.Name == "Add" && filepath.Base(r.sym.Path) == "a.go" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Add not among workspace symbols: %+v", m.wsym.rows)
		}
		h.keys("esc")
	})

	// F-169: with the setting on, gopls is told to emit its standard hints
	// and they are drawn inline without touching the buffer.
	t.Run("inlay hints", func(t *testing.T) {
		h.open("h.go")
		h.keys(":", "s", "e", "t", " ", "i", "n", "l", "a", "y", "h", "i", "n", "t", "s", "enter")
		h.wait(func() bool {
			v := plainView(m)
			return strings.Contains(v, "Add(a: 1, b: 2)")
		}, "parameter-name hints")
		v := plainView(m)
		t.Logf("hinted line: %q", func() string {
			for _, r := range strings.Split(v, "\n") {
				if strings.Contains(r, "Add(") {
					return r
				}
			}
			return ""
		}())
		if !strings.Contains(v, "v int := Add") && !strings.Contains(v, "v: int := Add") {
			t.Fatalf("no type hint after v:\n%s", v)
		}
		if got := m.active().Buffer().Line(3); got != "\tv := Add(1, 2)" {
			t.Fatalf("buffer changed: %q", got)
		}
		h.keys(":", "s", "e", "t", " ", "n", "o", "i", "n", "l", "a", "y", "h", "i", "n", "t", "s", "enter")
		if strings.Contains(plainView(m), "Add(a: 1") {
			t.Fatal("hints still drawn after :set noinlayhints")
		}
	})

	// Completion and signature help in a scratch buffer that is never saved.
	t.Run("completion", func(t *testing.T) {
		h.open("d.go")
		h.at(3, 1)
		h.keys("A") // append at end of the tab-only line
		h.keys("A", "d")
		h.keys("ctrl+space")
		h.wait(func() bool { return m.compOpen }, "completion popup")
		got := false
		for _, it := range m.compItems {
			if it.Label == "Add" {
				got = true
			}
		}
		if !got {
			t.Fatalf("completion lacks Add: %d items", len(m.compItems))
		}
		h.keys("enter")
		if line := m.active().Buffer().Line(3); !strings.Contains(line, "Add") {
			t.Fatalf("accepted line %q", line)
		}
	})

	t.Run("signature help", func(t *testing.T) {
		h.keys("(")
		h.wait(func() bool { return m.sigHelp != nil }, "signature help")
		sh := m.sigHelp
		t.Logf("signature %q params [%d:%d]", sh.Label, sh.ParamStart, sh.ParamEnd)
		if !strings.Contains(sh.Label, "Add") || sh.Label[sh.ParamStart:sh.ParamEnd] != "a int" {
			t.Fatalf("signature %q [%d:%d]", sh.Label, sh.ParamStart, sh.ParamEnd)
		}
		h.keys("1", ",")
		h.wait(func() bool {
			s := m.sigHelp
			return s != nil && s.ParamEnd <= len(s.Label) && s.Label[s.ParamStart:s.ParamEnd] == "b int"
		}, "second parameter active")
		// An open popup may take the first esc; leave insert mode either way.
		for i := 0; i < 3 && m.active().Mode() == textbuf.ModeInsert; i++ {
			h.keys("esc")
		}
		if m.active().Mode() == textbuf.ModeInsert {
			t.Fatal("still in insert mode after esc")
		}
	})

	// Closing the unsaved scratch buffer sends didClose.
	t.Run("didClose", func(t *testing.T) {
		h.keys(":", "q", "!", "enter")
		if m.active() != nil && m.active().Path() == h.path("d.go") {
			t.Fatalf("d.go still active:\n%s", plainView(m))
		}
	})

	// Format-on-save: gopls rewrites badly formatted code on :w; didSave and
	// the pending didChange reach the server first.
	t.Run("format on save", func(t *testing.T) {
		h.open("b.go")
		h.at(2, 0)
		h.keys("O")
		h.keys("v", "a", "r", " ", "z", "   ", "=", "   ", "1")
		h.keys("esc")
		h.keys(":", "w", "enter")
		h.wait(func() bool { return strings.Contains(h.readDisk("b.go"), "var z = 1") }, "formatted file on disk")
		if m.active().Buffer().Dirty() {
			t.Fatal("buffer still dirty after :w")
		}
	})

	// Rename from b.go (the declaration lives in the unopened a.go) edits both.
	t.Run("rename across files", func(t *testing.T) {
		// gd opened a.go; close it so the rename must edit it on disk (F-116).
		h.open("a.go")
		h.keys(":", "q", "enter")
		for _, tb := range m.bufs {
			if tb.ed != nil && tb.path == h.path("a.go") {
				t.Fatalf("a.go still open:\n%s", plainView(m))
			}
		}
		h.open("b.go")
		h.at(5, 9) // "return Add(1, 2)" moved down one line by the O above
		if got := m.active().Buffer().Line(5); !strings.Contains(got, "Add") {
			t.Fatalf("setup line = %q", got)
		}
		h.keys("g", "r")
		if !m.renameMode {
			t.Fatalf("rename input did not open:\n%s", plainView(m))
		}
		for _, k := range "Sum" {
			h.keys(string(k))
		}
		h.keys("enter")
		h.wait(func() bool { return strings.Contains(h.readDisk("a.go"), "func Sum") }, "a.go renamed on disk")
		h.wait(func() bool { return strings.Contains(m.active().Buffer().Text(), "Sum(1, 2)") }, "b.go buffer renamed")
	})
}
