package editor

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Call hierarchy, peek and semantic tokens (F-173) against a scripted server.

// serveF173 answers the call-hierarchy and semantic-token requests. Items
// live in the file the client opened, except "outer", which lives in
// outerURI so a jump to it is visible as a change of file.
func (f *goplusFake) serveF173(id *int64, method string, params json.RawMessage) {
	f.mu.Lock()
	uri, data, outer := f.lastOpen, f.semData, f.outerURI
	f.seen = append(f.seen, method)
	f.mu.Unlock()
	item := func(name string) map[string]any {
		u := uri
		if name == "outer" {
			u = outer
		}
		pos := map[string]any{"line": 0, "character": 8}
		return map[string]any{"name": name, "kind": 12, "uri": u, "data": "tok-" + name,
			"range":          map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": pos},
			"selectionRange": map[string]any{"start": pos, "end": pos}}
	}
	var p struct {
		Item struct {
			Name string `json:"name"`
			Data string `json:"data"`
		} `json:"item"`
	}
	json.Unmarshal(params, &p)
	site := map[string]any{"start": map[string]any{"line": 0, "character": 1}, "end": map[string]any{"line": 0, "character": 2}}
	switch method {
	case "textDocument/prepareCallHierarchy":
		f.reply(*id, []map[string]any{item("Helper")})
	case "callHierarchy/incomingCalls":
		switch p.Item.Name {
		case "Helper":
			f.reply(*id, []map[string]any{{"from": item("caller"), "fromRanges": []any{site, site}}})
		case "caller":
			f.reply(*id, []map[string]any{{"from": item("outer"), "fromRanges": []any{site}}})
		default:
			f.reply(*id, []map[string]any{})
		}
	case "callHierarchy/outgoingCalls":
		if p.Item.Name == "Helper" && p.Item.Data == "tok-Helper" { // the opaque data came back
			f.reply(*id, []map[string]any{{"to": item("leaf"), "fromRanges": []any{site}}})
		} else {
			f.reply(*id, []map[string]any{})
		}
	default:
		f.reply(*id, map[string]any{"data": data})
	}
}

func (f *goplusFake) sawRequest(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.seen {
		if s == method {
			return true
		}
	}
	return false
}

// openMainGoF173 is openMainGoWithLSP with the capabilities the F-173
// features need announced.
func openMainGoF173(t *testing.T, semData []int) (*Model, *goplusFake, string) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	helper := filepath.Join(ws.Root, "repos", "alpha", "src", "util", "helper.go")
	srv, mgr := startFakeServerCfg(t, "go", false, func(f *goplusFake) {
		f.outerURI = fileURI(helper)
		f.semData = semData
		f.extraCaps = map[string]any{
			"callHierarchyProvider": true,
			"semanticTokensProvider": map[string]any{
				"legend": map[string]any{"tokenTypes": []string{"namespace", "function", "variable"}, "tokenModifiers": []string{"readonly"}},
				"full":   true,
			},
		}
	})
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter") // open main.go
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")
	return m, srv, helper
}

func runCmd(m *Model, cmd string) {
	feed(m, ":")
	typeKeys(m, cmd)
	feed(m, "enter")
	runQueued(m)
}

func TestCallsIncomingTreeExpandsAndJumps(t *testing.T) {
	m, _, helper := openMainGoF173(t, nil)
	runCmd(m, "calls")
	if m.mode != modeCalls || m.calls == nil {
		t.Fatalf("calls picker did not open (mode %v): %s", m.mode, m.active().CommandLine())
	}
	if v := plainView(m); !strings.Contains(v, "incoming calls of Helper") || !strings.Contains(v, "caller") {
		t.Fatalf("root and its caller expected:\n%s", v)
	}
	if strings.Contains(plainView(m), "outer") {
		t.Fatal("the second level must wait for an expand")
	}
	feed(m, "j", "l") // select caller, open it
	runQueued(m)
	v := plainView(m)
	if !strings.Contains(v, "outer") || !strings.Contains(v, "×2") {
		t.Fatalf("expanding caller should list outer; call-site count ×2 on caller:\n%s", v)
	}
	feed(m, "h", "h") // close caller, then step to the parent
	if got := m.calls.rows()[m.calls.cur].item.Name; got != "Helper" {
		t.Fatalf("h on a closed node should go to its parent, on %q", got)
	}
	feed(m, "j", "l", "j", "enter") // reopen (no new request), select outer, jump
	if m.mode != modeNav || m.active().Path() != helper {
		t.Fatalf("enter should jump to outer in helper.go; mode %v path %s", m.mode, m.active().Path())
	}
	if c := m.active().Buffer().Cursor(); c.Line != 0 || c.Col != 8 {
		t.Fatalf("cursor %+v, want 0:8", c)
	}
}

func TestCallsOutgoing(t *testing.T) {
	m, srv, _ := openMainGoF173(t, nil)
	runCmd(m, "calls out")
	if v := plainView(m); !strings.Contains(v, "outgoing calls of Helper") || !strings.Contains(v, "leaf") {
		t.Fatalf("outgoing tree expected:\n%s", v)
	}
	if !srv.sawRequest("callHierarchy/outgoingCalls") || srv.sawRequest("callHierarchy/incomingCalls") {
		t.Fatalf("requests seen: %v", srv.seen)
	}
	feed(m, "esc")
	if m.mode != modeNav || m.calls != nil {
		t.Fatal("esc should close the tree")
	}
}

func TestCallsNeedServerSupport(t *testing.T) {
	// A server that reports capabilities without callHierarchyProvider is not asked.
	ws, _ := setupWorkspace(t)
	srv, mgr := startFakeServerCfg(t, "go", false, func(f *goplusFake) { f.extraCaps = map[string]any{"hoverProvider": true} })
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")
	runCmd(m, "calls")
	if m.mode == modeCalls || !strings.Contains(m.active().CommandLine(), "does not offer call hierarchy") {
		t.Fatalf("expected a named refusal, got mode %v %q", m.mode, m.active().CommandLine())
	}
	if srv.sawRequest("textDocument/prepareCallHierarchy") {
		t.Fatal("an unsupported request was sent")
	}
}

func TestPeekDefinitionStaysInBuffer(t *testing.T) {
	m, srv, helper := openMainGoF173(t, nil)
	main := m.active().Path()
	srv.mu.Lock()
	srv.locs = []map[string]any{{"uri": fileURI(helper), "range": map[string]any{
		"start": map[string]any{"line": 0, "character": 8}, "end": map[string]any{"line": 0, "character": 12}}}}
	srv.mu.Unlock()
	before := m.active().Buffer().Cursor()
	feed(m, "g", "p")
	runQueued(m)
	if m.peek == nil {
		t.Fatal("gp did not open the peek")
	}
	v := plainView(m)
	if !strings.Contains(v, "peek: definition of package") || !strings.Contains(v, "package util") {
		t.Fatalf("peek should show helper.go's text:\n%s", v)
	}
	if m.active().Path() != main {
		t.Fatal("peek left the buffer")
	}
	feed(m, "esc")
	if m.peek != nil || m.active().Path() != main || m.active().Buffer().Cursor() != before {
		t.Fatalf("esc should close the peek and change nothing: peek=%v cursor=%+v", m.peek, m.active().Buffer().Cursor())
	}
	feed(m, "g", "p")
	runQueued(m)
	feed(m, "enter")
	if m.peek != nil || m.active().Path() != helper {
		t.Fatalf("enter should jump to helper.go, on %s", m.active().Path())
	}
	if c := m.active().Buffer().Cursor(); c.Col != 8 {
		t.Fatalf("cursor %+v, want col 8", c)
	}
}

func TestPeekReferencesStepThroughLocations(t *testing.T) {
	m, srv, helper := openMainGoF173(t, nil)
	one := map[string]any{"uri": fileURI(helper), "range": map[string]any{
		"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 7}}}
	srv.mu.Lock()
	srv.locs = []map[string]any{one, one}
	srv.mu.Unlock()
	runCmd(m, "peek refs")
	if m.peek == nil || !strings.Contains(plainView(m), "references to package") || !strings.Contains(plainView(m), "(1/2)") {
		t.Fatalf("refs peek expected:\n%s", plainView(m))
	}
	feed(m, "down")
	if !strings.Contains(plainView(m), "(2/2)") {
		t.Fatalf("down should step to the second location:\n%s", plainView(m))
	}
	feed(m, "x") // any other key closes and carries on
	if m.peek != nil {
		t.Fatal("another key should close the peek")
	}
}

func TestPeekWithoutResultSaysSo(t *testing.T) {
	m, _, _ := openMainGoF173(t, nil)
	feed(m, "g", "p")
	runQueued(m)
	if m.peek != nil || !strings.Contains(m.active().CommandLine(), "no definition for package") {
		t.Fatalf("peek=%v msg=%q", m.peek, m.active().CommandLine())
	}
}

// pumpSemantic renders until the answer has been filed under the active tab.
func pumpSemantic(t *testing.T, m *Model) *bufTab {
	t.Helper()
	tab := m.bufs[m.activeTab]
	waitFor(t, m, func() bool { _ = m.View(); return len(tab.sem.lines) > 0 }, "semantic tokens")
	return tab
}

func TestSemanticTokensLayerOverSyntax(t *testing.T) {
	// "main" in "package main": a namespace.
	m, srv, _ := openMainGoF173(t, []int{0, 8, 4, 0, 0})
	m.View()
	if srv.sawRequest("textDocument/semanticTokens/full") {
		t.Fatal("semantic tokens are off by default; nothing should be asked")
	}
	runCmd(m, "set semantic")
	tab := pumpSemantic(t, m)
	raw := m.active().Buffer().Line(0)
	if raw != "package main" {
		t.Fatalf("fixture line = %q", raw)
	}
	base := "package main"
	styled := m.semanticLine(tab, Prefs{}, 0, raw, base)
	if styled == base || ansi.Strip(styled) != base {
		t.Fatalf("namespace token should add colour and keep the text: %q", styled)
	}
	if !strings.Contains(styled, theme.AccentDimText().Render("main")) {
		t.Fatalf("main should carry the theme's namespace style: %q", styled)
	}
	// An edited line keeps its syntax colours until the next answer.
	if got := m.semanticLine(tab, Prefs{}, 0, raw+"x", base+"x"); got != base+"x" {
		t.Fatalf("stale tokens were applied: %q", got)
	}
	// Off again: no overlay.
	runCmd(m, "set nosemantic")
	if got := m.semanticLine(tab, Prefs{}, 0, raw, base); got != base {
		t.Fatalf("overlay applied while off: %q", got)
	}
}

func TestSemanticTokensRefreshAfterEdit(t *testing.T) {
	m, srv, _ := openMainGoF173(t, []int{0, 8, 4, 0, 0})
	runCmd(m, "set semantic")
	tab := pumpSemantic(t, m)
	srv.mu.Lock()
	n := len(srv.seen)
	srv.mu.Unlock()
	feed(m, "A")
	typeKeys(m, "s")
	feed(m, "esc")
	waitFor(t, m, func() bool {
		_ = m.View()
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.seen) > n
	}, "a second token request after the edit")
	if tab.sem.seq != tab.ed.Buffer().Seq() {
		t.Fatal("the request should follow the buffer's newest state")
	}
}

func TestSemanticStyleMapping(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	for _, c := range []struct {
		tok lsp.SemanticToken
		ok  bool
	}{
		{lsp.SemanticToken{Type: "namespace"}, true},
		{lsp.SemanticToken{Type: "struct"}, true},
		{lsp.SemanticToken{Type: "method"}, true},
		{lsp.SemanticToken{Type: "parameter"}, true},
		{lsp.SemanticToken{Type: "variable", Modifiers: []string{"readonly"}}, true},
		{lsp.SemanticToken{Type: "variable"}, false},
		{lsp.SemanticToken{Type: "keyword"}, false},
		{lsp.SemanticToken{Type: "string"}, false},
	} {
		if _, ok := semanticStyle(c.tok); ok != c.ok {
			t.Errorf("%s %v: mapped=%v, want %v", c.tok.Type, c.tok.Modifiers, ok, c.ok)
		}
	}
}

func TestSemanticLineMultibyteAndTabs(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	tabStop = 4
	tab := &bufTab{}
	raw := "\tcafé := f(x)"
	st, _ := semanticStyle(lsp.SemanticToken{Type: "function"})
	tab.sem.lines = map[int]semLine{0: {text: raw, toks: []semTok{{start: 9, end: 10, st: st}}}} // the f
	text := expandTabs(raw)
	got := m0().semanticLine(tab, Prefs{SemanticTokens: true}, 0, raw, text)
	if ansi.Strip(got) != text || !strings.Contains(got, st.Render("f")) {
		t.Fatalf("token misplaced: %q (text %q)", got, text)
	}
}

func m0() *Model { return &Model{} }
