package editor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// scriptedSearcher returns canned hits synchronously.
type scriptedSearcher struct {
	hits []search.Hit
	err  error

	muCtx    any
	lastCtx  context.Context
	blocking bool
	release  chan struct{}
}

func (s *scriptedSearcher) Search(ctx context.Context, q string, roots []string) (<-chan search.Hit, error) {
	s.lastCtx = ctx
	if s.err != nil {
		return nil, s.err
	}
	if s.blocking {
		s.release = make(chan struct{})
		ch := make(chan search.Hit)
		go func() {
			<-s.release
			close(ch)
		}()
		return ch, nil
	}
	ch := make(chan search.Hit, len(s.hits))
	for _, h := range s.hits {
		ch <- h
	}
	close(ch)
	return ch, nil
}

func newSearchEditor(t *testing.T, ss *scriptedSearcher) (*Model, *workspace.Workspace) {
	t.Helper()
	base := newEditor(t)
	e := New("test", base.ws, WithSearcher(ss))
	e.Resize(100, 30)
	return e, base.ws
}

func vpathAbs(t *testing.T, ws *workspace.Workspace, vp string) string {
	t.Helper()
	parsed, err := workspace.ParseVPath(vp)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := ws.Resolve(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestSearchFlow(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	ss := &scriptedSearcher{hits: []search.Hit{
		{Path: vpathAbs(t, ws, "alpha/src/main.go"), Line: 3, Column: 0, Text: "needle here"},
		{Path: vpathAbs(t, ws, "beta/README.md"), Line: 1, Column: 4, Text: "the needle!"},
	}}
	m := New("test", ws, WithSearcher(ss))
	m.Resize(100, 30)

	if m.HandleKey("s") == false {
		t.Fatal("s did not open search")
	}
	if !strings.Contains(plainView(m), "search") {
		t.Fatalf("query overlay missing:\n%s", m.View())
	}
	for _, r := range "needle" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	if m.mode != modeResults {
		t.Fatalf("mode = %v, want results", m.mode)
	}

	for _, h := range ss.hits {
		m.Update(hitMsg(h))
	}
	m.Update(searchDoneMsg{})

	v := plainView(m)
	for _, want := range []string{"alpha/src/main.go:3", "beta/README.md:1", "2 hit(s)", "needle here"} {
		if !strings.Contains(v, want) {
			t.Errorf("results missing %q:\n%s", want, v)
		}
	}

	// Jump to second hit.
	m.HandleKey("down")
	m.HandleKey("enter")
	if m.openVPath != "beta/README.md" {
		t.Errorf("openVPath = %q, want beta/README.md", m.openVPath)
	}
	if !strings.Contains(plainView(m), "README.md") {
		t.Error("tree did not reveal jumped file")
	}
}

func TestSearchEscReturnsAndCancels(t *testing.T) {
	ss := &scriptedSearcher{blocking: true}
	m, _ := newSearchEditor(t, ss)
	m.HandleKey("s")
	for _, r := range "q" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	if !m.searching {
		t.Fatal("search not marked running")
	}
	m.HandleKey("esc")
	if m.mode != modeNav {
		t.Fatal("esc did not leave results")
	}
	select {
	case <-ss.lastCtx.Done():
	default:
		t.Fatal("context not cancelled on esc")
	}
}

func TestSearchErrorSurfaces(t *testing.T) {
	ss := &scriptedSearcher{err: errors.New("rg missing")}
	m, _ := newSearchEditor(t, ss)
	m.HandleKey("s")
	m.HandleKey("q")
	m.HandleKey("enter")
	if !strings.Contains(plainView(m), "rg missing") {
		t.Errorf("error not surfaced:\n%s", m.View())
	}
}

func TestSearchInertWithoutSearcher(t *testing.T) {
	m := newEditor(t)
	if m.HandleKey("s") {
		t.Error("s consumed without a searcher")
	}
}

func TestEmptyQueryNoop(t *testing.T) {
	ss := &scriptedSearcher{}
	m, _ := newSearchEditor(t, ss)
	m.HandleKey("s")
	m.HandleKey("enter")
	if m.mode == modeResults || m.searching {
		t.Error("empty query started a search")
	}
}
