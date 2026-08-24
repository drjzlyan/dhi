package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// fixture builds a one-member workspace ("api") containing main.go.
func fixture(t *testing.T) (*workspace.Workspace, *sandbox.Guard) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "api", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatalf("workspace.Create: %v", err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatalf("workspace.Load: %v", err)
	}
	jail, err := sandbox.NewJail(filepath.Join(root, "api"))
	if err != nil {
		t.Fatalf("jail: %v", err)
	}
	return ws, sandbox.NewGuard(jail, &sandbox.Policy{})
}

func pol(rules ...sandbox.Rule) *sandbox.Policy {
	return &sandbox.Policy{Rules: rules}
}

func input(t *testing.T, v map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadAllowAndDeny(t *testing.T) {
	ws, guard := fixture(t)
	guard.Policy = pol(sandbox.Rule{Op: sandbox.OpRead, Effect: sandbox.Allow})
	reg := New()
	for _, tool := range Builtins(Deps{WS: ws, Guard: guard}) {
		if err := reg.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	res := reg.Call(context.Background(), Call{ID: "1", Name: "read", Input: input(t, map[string]any{"path": "api/main.go"})})
	if res.IsError || res.Content != "package main\n" {
		t.Errorf("read = %+v", res)
	}

	// Default deny: nothing touches disk.
	res = reg.Call(context.Background(), Call{ID: "2", Name: "write", Input: input(t, map[string]any{"path": "api/evil.txt", "content": "nope"})})
	if !res.IsError {
		t.Errorf("write should be denied by default: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(ws.Members()[0].Path, "evil.txt")); !os.IsNotExist(err) {
		t.Error("denied write touched disk")
	}
}

func TestOutsideVocabRejected(t *testing.T) {
	ws, guard := fixture(t)
	guard.Policy = pol(sandbox.Rule{Op: sandbox.OpRead, Effect: sandbox.Allow})
	reg := New()
	for _, tool := range Builtins(Deps{WS: ws, Guard: guard}) {
		reg.Register(tool)
	}
	for _, bad := range []string{"/etc/passwd", "../secrets", "api/../../etc"} {
		res := reg.Call(context.Background(), Call{Name: "read", Input: input(t, map[string]any{"path": bad})})
		if !res.IsError {
			t.Errorf("read %q should fail, got %+v", bad, res)
		}
	}
}

func TestWriteAskApproval(t *testing.T) {
	ws, guard := fixture(t)
	guard.Policy = pol(
		sandbox.Rule{Op: sandbox.OpRead, Effect: sandbox.Allow},
		sandbox.Rule{Op: sandbox.OpWrite, Path: "docs/**", Effect: sandbox.Ask},
	)
	ap := NewApprovals()
	signals := make(chan *Approval, 8)
	ap.OnRequest = func(a *Approval) { signals <- a }
	reg := New()
	for _, tool := range Builtins(Deps{WS: ws, Guard: guard, Approvals: ap, AgentID: "scout"}) {
		reg.Register(tool)
	}

	// Approved write lands byte-exact.
	done := make(chan Result, 1)
	go func() {
		done <- reg.Call(context.Background(), Call{ID: "w1", Name: "write",
			Input: input(t, map[string]any{"path": "api/docs/notes.md", "content": "hello"})})
	}()
	select {
	case <-signals:
	case <-time.After(2 * time.Second):
		t.Fatal("no approval surfaced")
	}
	pending := ap.List()
	if len(pending) != 1 || pending[0].Agent != "scout" || pending[0].Target != "api/docs/notes.md" {
		t.Fatalf("pending = %+v", pending)
	}
	if !ap.Resolve(pending[0].ID, true) {
		t.Fatal("resolve failed")
	}
	select {
	case res := <-done:
		if res.IsError {
			t.Errorf("approved write errored: %s", res.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write did not complete")
	}
	data, err := os.ReadFile(filepath.Join(ws.Members()[0].Path, "docs", "notes.md"))
	if err != nil || string(data) != "hello" {
		t.Errorf("file = %q, err %v; want \"hello\"", data, err)
	}

	// Denied ask never touches disk.
	go func() {
		done <- reg.Call(context.Background(), Call{ID: "w2", Name: "write",
			Input: input(t, map[string]any{"path": "api/docs/nope.md", "content": "x"})})
	}()
	<-signals
	id := ap.List()[0].ID
	ap.Resolve(id, false)
	select {
	case res := <-done:
		if !res.IsError {
			t.Errorf("denied write succeeded: %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write did not return")
	}
	if _, err := os.Stat(filepath.Join(ws.Members()[0].Path, "docs", "nope.md")); !os.IsNotExist(err) {
		t.Error("denied write touched disk")
	}
}

type fakeSearcher struct {
	hits []search.Hit
}

func (f fakeSearcher) Search(_ context.Context, query string, roots []string) (<-chan search.Hit, error) {
	ch := make(chan search.Hit, len(f.hits))
	defer close(ch)
	for _, h := range f.hits {
		ch <- h
	}
	return ch, nil
}

func TestSearchTool(t *testing.T) {
	ws, guard := fixture(t)
	guard.Policy = pol(sandbox.Rule{Op: sandbox.OpRead, Effect: sandbox.Allow})
	memberPath := ws.Members()[0].Path
	s := fakeSearcher{hits: []search.Hit{
		{Path: filepath.Join(memberPath, "main.go"), Line: 1, Text: "package main"},
	}}
	reg := New()
	for _, tool := range Builtins(Deps{WS: ws, Guard: guard, Searcher: s}) {
		reg.Register(tool)
	}
	res := reg.Call(context.Background(), Call{Name: "search", Input: input(t, map[string]any{"query": "package"})})
	if res.IsError {
		t.Fatalf("search: %s", res.Content)
	}
	if res.Content != "api/main.go:1:package main" {
		t.Errorf("search = %q, want vpath-mapped hit", res.Content)
	}
	if got := reg.Call(context.Background(), Call{Name: "search", Input: input(t, map[string]any{"query": "x", "member": "ghost"})}); !got.IsError {
		t.Errorf("unknown member should error: %+v", got)
	}
}

func TestListTool(t *testing.T) {
	ws, guard := fixture(t)
	guard.Policy = pol(sandbox.Rule{Op: sandbox.OpRead, Effect: sandbox.Allow})
	reg := New()
	for _, tool := range Builtins(Deps{WS: ws, Guard: guard}) {
		reg.Register(tool)
	}
	res := reg.Call(context.Background(), Call{Name: "list", Input: input(t, map[string]any{"path": "api"})})
	if res.IsError {
		t.Fatalf("list: %s", res.Content)
	}
	if res.Content != "main.go\nsub/" {
		t.Errorf("list = %q", res.Content)
	}
}

func TestRegistry(t *testing.T) {
	reg := New()
	dup := stubTool{name: "x"}
	if err := reg.Register(dup); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(stubTool{name: "x"}); err == nil {
		t.Error("duplicate registration accepted")
	}
	if err := reg.Register(stubTool{name: "a"}); err != nil {
		t.Fatal(err)
	}
	defs := reg.Defs([]string{"a", "x", "missing"})
	if len(defs) != 2 || defs[0].Name != "a" || defs[1].Name != "x" {
		t.Errorf("Defs order/filter wrong: %+v", defs)
	}
	res := reg.Call(context.Background(), Call{Name: "ghost"})
	if !res.IsError {
		t.Error("unknown tool should error")
	}
	res = reg.Call(context.Background(), Call{Name: "boom", Input: json.RawMessage(`{}`)})
	if !res.IsError || res.Content == "" {
		t.Errorf("panic not converted to error result: %+v", res)
	}
}

type stubTool struct{ name string }

func (s stubTool) Def() Def { return Def{Name: s.name} }

func (s stubTool) Exec(_ context.Context, _ json.RawMessage) (string, error) {
	if s.name == "boom" {
		panic("kaboom")
	}
	return "ok", nil
}
