package editor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/lsp"
)

func (f *goplusFake) openedLang() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastLang
}

func TestFilesReachTheirOwnServerWithTheirOwnLanguageID(t *testing.T) {
	ts, mgr := startFakeServerFor(t, "typescript", false)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))

	if m.clientFor("/p/a.tsx") == nil || m.clientFor("/p/a.js") == nil {
		t.Fatal("TypeScript/JavaScript files do not reach the typescript client")
	}
	if m.clientFor("/p/a.go") != nil || m.clientFor("/p/README.md") != nil || m.clientFor("/p/a.py") != nil {
		t.Fatal("a file reached a server of another language")
	}
	m.lspOpenDoc("/p/a.tsx", "const a = <div/>")
	deadline := time.Now().Add(15 * time.Second)
	for ts.openedLang() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := ts.openedLang(); got != "typescriptreact" {
		t.Fatalf("didOpen languageId = %q, want typescriptreact", got)
	}
}

// fakeTools is a HostTools whose tool counts as installed once its
// marker file exists (the install command creates it).
type fakeTools struct {
	marker    string
	refreshed int
	mu        sync.Mutex
}

func (f *fakeTools) Find(tool string) (string, bool) {
	if _, err := os.Stat(f.marker); err == nil {
		return f.marker, true
	}
	return "", false
}

func (f *fakeTools) Plan(tool string) ToolPlan {
	return ToolPlan{Tool: tool, Purpose: "testing", Argv: []string{"/bin/sh", "-c", "touch " + f.marker}}
}

func (f *fakeTools) Refresh() {
	f.mu.Lock()
	f.refreshed++
	f.mu.Unlock()
}

func TestMissingServerIsReportedOncePerLanguageAndNothingInstalls(t *testing.T) {
	tools := &fakeTools{marker: filepath.Join(t.TempDir(), "installed")}
	m := openGoWithManager(t, lsp.NewManager(nil, nil), WithHostTools(tools))
	e := m.active()

	m.lspOpenDoc("/p/a.ts", "x")
	if msg := e.Message(); !strings.Contains(msg, "TypeScript") || !strings.Contains(msg, ":lsp install typescript") {
		t.Fatalf("note = %q", msg)
	}
	e.SetMessage("")
	m.lspOpenDoc("/p/b.ts", "y") // same language: already told
	if e.Message() != "" {
		t.Fatalf("repeated the notice: %q", e.Message())
	}
	m.lspOpenDoc("/p/a.py", "z") // another language gets its own
	if !strings.Contains(e.Message(), "Python") {
		t.Fatalf("python note = %q", e.Message())
	}
	if m.toolCard != nil || len(m.terms) != 0 {
		t.Fatal("opening files raised a card or ran an install")
	}
}

// :lsp install raises the card with the exact command; it runs only on
// y, in a terminal tab, and DHI looks for the tool again when it exits.
func TestLSPInstallRunsTheApprovedCommandInATerminalTab(t *testing.T) {
	tools := &fakeTools{marker: filepath.Join(t.TempDir(), "installed")}
	m := openGoWithManager(t, lsp.NewManager(nil, nil), WithHostTools(tools), WithTermEnv(os.Environ()))
	e := m.active()

	m.ExecEx(e, "lsp install typescript")
	if m.toolCard == nil || m.toolCard.Tool != "typescript-language-server" {
		t.Fatalf("card = %+v", m.toolCard)
	}
	view := plainView(m)
	for _, want := range []string{"install typescript-language-server", "touch ", "y run it in a terminal tab"} {
		if !strings.Contains(view, want) {
			t.Errorf("card lacks %q", want)
		}
	}
	m.HandleKey("x") // the card traps keys
	if len(m.terms) != 0 {
		t.Fatal("ran without a yes")
	}
	m.HandleKey("y")
	if m.toolCard != nil || len(m.terms) != 1 || m.terms[0].tool != "typescript-language-server" || !m.drawerOpen {
		t.Fatalf("install tab not opened: card=%v terms=%d", m.toolCard, len(m.terms))
	}
	waitFor(t, m, func() bool { return strings.Contains(e.Message(), "is installed") }, "outcome after the tab exits")

	m.ExecEx(e, "lsp install typescript")
	if m.toolCard == nil {
		t.Fatal("card closed")
	}
	m.HandleKey("n")
	if m.toolCard != nil {
		t.Fatal("n must close the card")
	}
	for _, c := range []struct{ cmd, want string }{
		{"lsp install", "name a language"},
		{"lsp install cobol", "unknown language"},
	} {
		m.ExecEx(e, c.cmd)
		if !strings.Contains(e.Message(), c.want) {
			t.Errorf("%q → %q, want %q", c.cmd, e.Message(), c.want)
		}
	}
}

// A failed install says so and points at its tab.
func TestFailedInstallIsNamed(t *testing.T) {
	tools := &failTools{fakeTools{marker: filepath.Join(t.TempDir(), "never")}}
	m := openGoWithManager(t, lsp.NewManager(nil, nil), WithHostTools(tools), WithTermEnv(os.Environ()))
	m.ExecEx(m.active(), "lsp install yaml")
	m.HandleKey("y")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "failed (exit 2)") }, "failure note")
	tools.mu.Lock()
	defer tools.mu.Unlock()
	if tools.refreshed != 1 {
		t.Fatalf("a missing tool re-reads the shell env once, got %d", tools.refreshed)
	}
}

type failTools struct{ fakeTools }

func (f *failTools) Plan(tool string) ToolPlan {
	return ToolPlan{Tool: tool, Purpose: "testing", Argv: []string{"/bin/sh", "-c", "exit 2"}}
}

func TestFormattingNeedsTheServerToAdvertiseIt(t *testing.T) {
	srv, mgr := startFakeServerFor(t, "go", true) // like pyright: no documentFormattingProvider
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "open")
	srv.mu.Lock()
	srv.formatEdits = renameMainEdit()
	srv.mu.Unlock()

	note := m.formatEditor(m.active())
	if !strings.Contains(note, "cannot format") || !strings.Contains(note, "editor.languages.go.formatter") {
		t.Fatalf("note = %q", note)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, ev := range srv.events {
		if ev == "formatting" {
			t.Fatal("asked a server that does not format to format")
		}
	}
}

func TestConfiguredFormatterRunsInTheUsersEnvironment(t *testing.T) {
	reg, _ := langserver.Resolve(map[string]langserver.Override{"go": {Formatter: []string{"tr", "a-z", "A-Z"}}})
	m := openGoWithManager(t, lsp.NewManager(nil, nil), WithLanguages(reg))
	e := m.active()

	if note := m.formatEditor(e); !strings.Contains(note, "no environment") {
		t.Fatalf("note without env = %q", note)
	}
	WithTermEnv([]string{"PATH=/nonexistent"})(m)
	if note := m.formatEditor(e); !strings.Contains(note, "not on your PATH") {
		t.Fatalf("note with an empty PATH = %q", note)
	}

	WithTermEnv([]string{"PATH=/usr/bin:/bin"})(m)
	before := e.Buffer().Text()
	if note := m.formatEditor(e); note != "" {
		t.Fatalf("format: %q", note)
	}
	if got := e.Buffer().Text(); got != strings.ToUpper(before) {
		t.Fatalf("buffer = %q, want %q", got, strings.ToUpper(before))
	}
	e.Buffer() // one undo step restores it
	feed(m, "u")
	if e.Buffer().Text() != before {
		t.Fatalf("undo = %q", e.Buffer().Text())
	}
}

func TestFormatterFailureKeepsTheTextAndSaysWhy(t *testing.T) {
	reg, _ := langserver.Resolve(map[string]langserver.Override{"go": {Formatter: []string{"false"}}})
	m := openGoWithManager(t, lsp.NewManager(nil, nil), WithLanguages(reg))
	WithTermEnv([]string{"PATH=/usr/bin:/bin"})(m)
	before := m.active().Buffer().Text()
	if note := m.formatEditor(m.active()); !strings.Contains(note, "format failed") {
		t.Fatalf("note = %q", note)
	}
	if m.active().Buffer().Text() != before {
		t.Fatal("a failed formatter changed the buffer")
	}
}

// openGoWithManager opens src/main.go in an editor built with mgr + opts.
func openGoWithManager(t *testing.T, mgr *lsp.Manager, opts ...Option) *Model {
	t.Helper()
	ws, _ := setupWorkspace(t)
	m := New("test", ws, append([]Option{WithLSP(mgr)}, opts...)...)
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	if m.active() == nil {
		t.Fatal("no buffer opened")
	}
	return m
}

func TestInstallCommandRaisesTheCard(t *testing.T) {
	tools := &fakeTools{marker: filepath.Join(t.TempDir(), "installed")}
	m := openGoWithManager(t, lsp.NewManager(nil, nil), WithHostTools(tools))
	m.ExecEx(m.active(), "install bwrap")
	if m.toolCard == nil || m.toolCard.Tool != "bwrap" {
		t.Fatalf("card = %+v", m.toolCard)
	}
	m.HandleKey("n")
	m.ExecEx(m.active(), "install")
	if !strings.Contains(m.active().Message(), "name a tool") {
		t.Fatalf("msg = %q", m.active().Message())
	}
}
