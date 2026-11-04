package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func symEditor(t *testing.T) (*Model, *goplusFake) {
	t.Helper()
	srv, mgr := startFakeServer(t)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)
	return m, srv
}

func wsSym(name string, kind int, uri string, line int) map[string]any {
	return map[string]any{"name": name, "kind": kind, "containerName": "pkg",
		"location": map[string]any{"uri": uri, "range": map[string]any{
			"start": map[string]any{"line": line, "character": 0},
			"end":   map[string]any{"line": line, "character": 4}}}}
}

func TestSymbolsWorkspaceMemberPathsAndDropsOutside(t *testing.T) {
	m, srv := symEditor(t)
	file := filepath.Join(m.members[0].path, "src", "main.go")
	srv.wsSyms = []map[string]any{
		wsSym("Load", 12, fileURI(file), 6),
		wsSym("Println", 12, "file:///usr/local/go/src/fmt/print.go", 1),
	}
	out, err := m.LSPCall(context.Background(), "symbols_workspace", "", 0, 0, "Load")
	if err != nil {
		t.Fatal(err)
	}
	want := m.members[0].name + "/src/main.go:7"
	if !strings.Contains(out, "func pkg.Load") || !strings.Contains(out, want) {
		t.Fatalf("out = %q, want func pkg.Load at %s", out, want)
	}
	if strings.Contains(out, "Println") {
		t.Fatalf("symbol outside every member leaked: %q", out)
	}
}

func TestSymbolsWorkspaceCapsAt200(t *testing.T) {
	m, srv := symEditor(t)
	file := fileURI(filepath.Join(m.members[0].path, "app.go"))
	for i := 0; i < 250; i++ {
		srv.wsSyms = append(srv.wsSyms, wsSym(fmt.Sprintf("S%d", i), 12, file, i))
	}
	out, err := m.LSPCall(context.Background(), "symbols_workspace", "", 0, 0, "S")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "\nfunc ") + 1; n != symbolCap {
		t.Fatalf("lines = %d, want %d", n, symbolCap)
	}
	if !strings.Contains(out, "truncated: showing 200 of 250") {
		t.Fatalf("no truncation note: %q", out[len(out)-120:])
	}
}

func TestSymbolsDocumentOutline(t *testing.T) {
	m, _ := symEditor(t)
	file := filepath.Join(m.members[0].path, "app.go")
	out, err := m.LSPCall(context.Background(), "symbols_document", file, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "func main") || !strings.Contains(out, "  method Config.Load") && !strings.Contains(out, "  method Load") {
		t.Fatalf("outline = %q", out)
	}
}

func TestSymbolsNoServerIsAPlainAnswer(t *testing.T) {
	m := newEditor(t) // no manager at all
	file := filepath.Join(m.members[0].path, "app.go")
	out, err := m.LSPCall(context.Background(), "symbols_document", file, 0, 0, "")
	if err != nil || !strings.Contains(out, "no language server for Go") {
		t.Fatalf("out = %q err = %v", out, err)
	}
	out, err = m.LSPCall(context.Background(), "symbols_document", "/x/notes.txt", 0, 0, "")
	if err != nil || !strings.Contains(out, "no language server for .txt") {
		t.Fatalf("out = %q err = %v", out, err)
	}
	out, err = m.LSPCall(context.Background(), "symbols_workspace", "", 0, 0, "x")
	if err != nil || !strings.Contains(out, "no language server is running") {
		t.Fatalf("out = %q err = %v", out, err)
	}
}
