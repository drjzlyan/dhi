package editor

import (
	"context"
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
	deadline := time.Now().Add(2 * time.Second)
	for ts.openedLang() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := ts.openedLang(); got != "typescriptreact" {
		t.Fatalf("didOpen languageId = %q, want typescriptreact", got)
	}
}

func TestMissingServerIsReportedOncePerLanguageAndNothingInstalls(t *testing.T) {
	var installs int
	var mu sync.Mutex
	mgr := lsp.NewManager(t.TempDir(), nil)
	mgr.SetManagedRoot(t.TempDir())
	m := openGoWithManager(t, mgr, WithLSPInstaller(func(context.Context, string, []string) error {
		mu.Lock()
		installs++
		mu.Unlock()
		return nil
	}))
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
	mu.Lock()
	defer mu.Unlock()
	if installs != 0 {
		t.Fatalf("opening files installed %d time(s)", installs)
	}
}

func TestLSPInstallShowsThePlanAndInstallsOnlyOnYes(t *testing.T) {
	type call struct {
		prefix string
		pkgs   []string
	}
	calls := make(chan call, 4)
	mgr := lsp.NewManager(t.TempDir(), nil)
	mgr.SetManagedRoot(t.TempDir())
	m := openGoWithManager(t, mgr, WithLSPInstaller(func(_ context.Context, prefix string, pkgs []string) error {
		calls <- call{prefix, pkgs}
		return nil
	}))
	e := m.active()

	m.ExecEx(e, "lsp install typescript")
	plan := e.Message()
	for _, want := range []string{"typescript-language-server@6.0.1", "typescript@5.9.3", mgr.ManagedPrefix("typescript"), "yes"} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan lacks %q: %s", want, plan)
		}
	}
	select {
	case <-calls:
		t.Fatal("installed without confirmation")
	case <-time.After(100 * time.Millisecond):
	}

	m.ExecEx(e, "lsp install typescript yes")
	select {
	case c := <-calls:
		if c.prefix != mgr.ManagedPrefix("typescript") || len(c.pkgs) != 2 {
			t.Fatalf("install call = %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("confirmed install never ran")
	}
	waitFor(t, m, func() bool { return !m.lspInstalling["typescript"] }, "install flag cleared")
	if !strings.Contains(e.Message(), "installed") && !strings.Contains(e.Message(), "language server") {
		t.Errorf("outcome not shown: %q", e.Message())
	}

	for _, c := range []struct{ cmd, want string }{
		{"lsp install", "name a language"},
		{"lsp install cobol", "unknown language"},
		{"lsp install go", "toolchain"},
		{"lsp install python x", "pyright@1.1.414"},
	} {
		cmd, want := c.cmd, c.want
		m.ExecEx(e, cmd)
		if !strings.Contains(e.Message(), want) {
			t.Errorf("%q → %q, want %q", cmd, e.Message(), want)
		}
	}
}

func TestInstallFailureIsNamedAndClearsTheFlag(t *testing.T) {
	mgr := lsp.NewManager(t.TempDir(), nil)
	mgr.SetManagedRoot(t.TempDir())
	m := openGoWithManager(t, mgr, WithLSPInstaller(func(context.Context, string, []string) error {
		return context.DeadlineExceeded
	}))
	m.ExecEx(m.active(), "lsp install yaml yes")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "failed") }, "failure note")
	if m.lspInstalling["yaml"] {
		t.Fatal("flag stuck after a failed install")
	}
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

func TestConfiguredFormatterRunsInTheHermeticEnvironmentOnly(t *testing.T) {
	reg, _ := langserver.Resolve(map[string]langserver.Override{"go": {Formatter: []string{"tr", "a-z", "A-Z"}}})
	m := openGoWithManager(t, lsp.NewManager(t.TempDir(), nil), WithLanguages(reg))
	e := m.active()

	// No hermetic environment: refuse by name, never use the host PATH.
	if note := m.formatEditor(e); !strings.Contains(note, "hermetic") {
		t.Fatalf("note without env = %q", note)
	}
	m.termEnv = []string{"PATH=/nonexistent"}
	if note := m.formatEditor(e); !strings.Contains(note, "not found in DHI's environment") {
		t.Fatalf("note with an empty PATH = %q", note)
	}

	m.termEnv = []string{"PATH=/usr/bin:/bin"}
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
	m := openGoWithManager(t, lsp.NewManager(t.TempDir(), nil), WithLanguages(reg))
	m.termEnv = []string{"PATH=/usr/bin:/bin"}
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
