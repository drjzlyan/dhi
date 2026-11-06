package editor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// F-181 (ADR-0051): ghost text from the language server's inline
// completion, and the explicit :suggest agent call.

// ghostModel opens text as app.go against a fake server that advertises
// inline completion and answers with items; on is editor.inline_completion.
func ghostModel(t *testing.T, text string, items any, wrap, on bool) (*Model, *goplusFake) {
	t.Helper()
	prev := ghostDebounce
	ghostDebounce = 0
	t.Cleanup(func() { ghostDebounce = prev })
	srv, mgr := startFakeServerCfg(t, "go", false, func(f *goplusFake) {
		f.extraCaps = map[string]any{"inlineCompletionProvider": true}
		f.inlineItems = items
	})
	m := newEditor(t)
	m.lspMgr = mgr
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.active() == nil {
		t.Fatal("app.go did not open")
	}
	m.prefs = func() Prefs {
		return Prefs{TabWidth: 4, LineNumbers: true, Wrap: wrap, InlineCompletion: on, Scrollback: 1000}
	}
	return m, srv
}

func inlineAsks(srv *goplusFake) int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.inlineAsks
}

func ghostShown(m *Model) bool { return m.ghostFor(m.active()) != nil }

func lineAt(m *Model, l int) string { return m.active().Buffer().Line(l) }

func plainItem(text string) map[string]any { return map[string]any{"insertText": text} }

// A server proposal shows dimmed after the cursor without touching the
// buffer; tab inserts it as one undo step.
func TestGhostTextFromServerShowsAndTabAccepts(t *testing.T) {
	m, srv := ghostModel(t, "x := compute(\n", []map[string]any{plainItem("1, 2)")}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	// The cursor block is the blank cell between the text and the ghost.
	if v := plainView(m); !strings.Contains(v, "x := compute( 1, 2)") {
		t.Fatalf("ghost not drawn:\n%s", v)
	}
	if got := lineAt(m, 0); got != "x := compute(" {
		t.Fatalf("buffer changed by a suggestion: %q", got)
	}
	if c := m.active().Buffer().Cursor(); c.Col != 13 {
		t.Fatalf("cursor col %d, want 13 (the ghost is not text)", c.Col)
	}
	srv.mu.Lock()
	at := srv.inlineAt[0]
	srv.mu.Unlock()
	if at != "0:13" {
		t.Fatalf("asked at %s, want 0:13", at)
	}
	feed(m, "tab")
	if got := lineAt(m, 0); got != "x := compute(1, 2)" || ghostShown(m) {
		t.Fatalf("after tab: %q ghost=%v", got, ghostShown(m))
	}
	if c := m.active().Buffer().Cursor(); c.Col != 18 {
		t.Fatalf("cursor col %d after accept, want 18", c.Col)
	}
	feed(m, "esc", "u")
	if got := lineAt(m, 0); got != "x := compute(" {
		t.Fatalf("accept is one undo step, got %q", got)
	}
}

// Only insert mode shows ghost text, and esc drops it.
func TestGhostTextEscDismissesAndNormalModeShowsNone(t *testing.T) {
	m, _ := ghostModel(t, "x := compute(\n", []map[string]any{plainItem("1, 2)")}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	feed(m, "esc")
	if m.ghost != nil || strings.Contains(plainView(m), "1, 2)") {
		t.Fatalf("esc left the ghost:\n%s", plainView(m))
	}
}

// Typing the suggestion's next character walks through it; another
// character drops it.
func TestGhostTextTypingPast(t *testing.T) {
	m, srv := ghostModel(t, "x := compute(\n", []map[string]any{plainItem("1, 2)")}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	asks := inlineAsks(srv)
	feed(m, "1")
	if !ghostShown(m) || m.ghost.text != ", 2)" {
		t.Fatalf("typing the next character must keep the rest: %+v", m.ghost)
	}
	if v := plainView(m); !strings.Contains(v, "x := compute(1 , 2)") {
		t.Fatalf("view:\n%s", v)
	}
	feed(m, "q")
	if m.ghost != nil {
		t.Fatalf("another character must drop it: %+v", m.ghost)
	}
	if lineAt(m, 0) != "x := compute(1q" {
		t.Fatalf("buffer %q", lineAt(m, 0))
	}
	_ = asks
}

// The ghost is gone and a new request follows the edit.
func TestGhostTextRequestedAgainAfterATypedEdit(t *testing.T) {
	m, srv := ghostModel(t, "x := compute(\n", []map[string]any{plainItem("1, 2)")}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return inlineAsks(srv) >= 1 }, "first request")
	feed(m, "q")
	settle(t, m, func() bool { return inlineAsks(srv) >= 2 }, "second request")
	srv.mu.Lock()
	at := srv.inlineAt[len(srv.inlineAt)-1]
	srv.mu.Unlock()
	if at != "0:14" {
		t.Fatalf("second request at %s, want 0:14", at)
	}
}

func TestGhostTextOffNeverRequestsAndSetCommandTogglesTheSession(t *testing.T) {
	m, srv := ghostModel(t, "x := compute(\n", []map[string]any{plainItem("1, 2)")}, false, false)
	feed(m, "A")
	feed(m, "q", "backspace")
	for i := 0; i < 8; i++ {
		m.View()
		m.drainTerm()
		time.Sleep(10 * time.Millisecond)
	}
	if inlineAsks(srv) != 0 || m.ghost != nil {
		t.Fatalf("asked while off (asks=%d)", inlineAsks(srv))
	}
	feed(m, "esc", ":")
	typeKeys(m, "set ghost")
	feed(m, "enter")
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost after :set ghost")
	feed(m, "esc", ":")
	typeKeys(m, "set noghost")
	feed(m, "enter")
	if m.ghost != nil || m.ghostEnabled(m.editorPrefs()) {
		t.Fatal(":set noghost must switch it off")
	}
}

func TestGhostTextWithoutCapabilityIsNeverRequested(t *testing.T) {
	prev := ghostDebounce
	ghostDebounce = 0
	t.Cleanup(func() { ghostDebounce = prev })
	srv, mgr := startFakeServerCfg(t, "go", false, func(f *goplusFake) { f.inlineItems = []map[string]any{plainItem("zzz")} })
	m := newEditor(t)
	m.lspMgr = mgr
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	if err := os.WriteFile(path, []byte("x := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	feed(m, "A", "q")
	for i := 0; i < 8; i++ {
		m.View()
		m.drainTerm()
		time.Sleep(10 * time.Millisecond)
	}
	if inlineAsks(srv) != 0 || m.ghost != nil {
		t.Fatalf("asked a server without inlineCompletionProvider (asks=%d)", inlineAsks(srv))
	}
}

// A proposal that re-types the word being typed shows only what follows.
func TestGhostTextRangeRetypingThePrefix(t *testing.T) {
	item := map[string]any{"insertText": "compute(1)", "range": map[string]any{
		"start": map[string]any{"line": 0, "character": 5}, "end": map[string]any{"line": 0, "character": 10}}}
	m, _ := ghostModel(t, "x := compu\n", []map[string]any{item}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	if m.ghost.text != "te(1)" {
		t.Fatalf("ghost = %q, want te(1)", m.ghost.text)
	}
	feed(m, "tab")
	if got := lineAt(m, 0); got != "x := compute(1)" {
		t.Fatalf("line %q", got)
	}
}

// A proposal that does not extend what is typed has no ghost.
func TestGhostTextDroppedWhenItDoesNotExtendTheTypedWord(t *testing.T) {
	item := map[string]any{"insertText": "other()", "range": map[string]any{
		"start": map[string]any{"line": 0, "character": 5}, "end": map[string]any{"line": 0, "character": 10}}}
	m, srv := ghostModel(t, "x := compu\n", []map[string]any{item}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return inlineAsks(srv) >= 1 }, "request")
	for i := 0; i < 5; i++ {
		m.View()
		m.drainTerm()
		time.Sleep(10 * time.Millisecond)
	}
	if m.ghost != nil {
		t.Fatalf("ghost for a proposal that rewrites the word: %+v", m.ghost)
	}
}

// A snippet proposal shows its expansion and accepting starts the session.
func TestGhostTextSnippetAcceptStartsASession(t *testing.T) {
	item := map[string]any{"insertText": map[string]any{"kind": "snippet", "value": "compute(${1:a}, ${2:b})$0"}}
	m, _ := ghostModel(t, "x := \n", []map[string]any{item}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	if v := plainView(m); !strings.Contains(v, "x :=  compute(a, b)") {
		t.Fatalf("snippet expansion not shown:\n%s", v)
	}
	feed(m, "tab")
	if lineAt(m, 0) != "x := compute(a, b)" || m.snip == nil {
		t.Fatalf("line %q session %v", lineAt(m, 0), m.snip)
	}
}

// More lines than one: the first is drawn with a marker, all are inserted.
func TestGhostTextMultiLineInsertsEverything(t *testing.T) {
	m, _ := ghostModel(t, "func f() {\n", []map[string]any{plainItem("\n\treturn 1\n}")}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	feed(m, "tab")
	if got := m.active().Buffer().Text(); got != "func f() {\n\treturn 1\n}\n" {
		t.Fatalf("buffer %q", got)
	}
}

// Ghost text does not show between the cursor and following text.
func TestGhostTextOnlyAtTheEndOfALine(t *testing.T) {
	m, srv := ghostModel(t, "x := compute(1)\n", []map[string]any{plainItem("zz")}, false, true)
	feed(m, "0", "i", "q")
	settle(t, m, func() bool { return inlineAsks(srv) >= 1 }, "request")
	for i := 0; i < 5; i++ {
		m.View()
		m.drainTerm()
		time.Sleep(10 * time.Millisecond)
	}
	if ghostShown(m) || strings.Contains(plainView(m), "zz") {
		t.Fatal("ghost drawn in the middle of a line")
	}
	feed(m, "tab")
	if !strings.Contains(lineAt(m, 0), "\t") {
		t.Fatalf("tab must indent when nothing shows: %q", lineAt(m, 0))
	}
}

// F-171 x F-181: tab goes to the completion popup, then a snippet stop,
// then ghost text, then indent.
func TestGhostTabPrecedence(t *testing.T) {
	m, _ := ghostModel(t, "x := \n", []map[string]any{plainItem("compute()")}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	// 1. the popup beats the ghost
	m.compItems = []lsp.CompletionItem{{Label: "zz", Insert: "zz"}}
	m.compOpen = true
	if ghostShown(m) {
		t.Fatal("ghost and popup must not both show")
	}
	feed(m, "tab")
	if m.compOpen || lineAt(m, 0) != "x := zz" {
		t.Fatalf("popup tab: %q", lineAt(m, 0))
	}
	if m.ghost != nil && ghostShown(m) {
		t.Fatal("the stale ghost survived the popup accept")
	}
	// 2. a snippet session beats the ghost
	e := m.active()
	m.expandSnippet(e, e.Buffer().Cursor(), e.Buffer().Cursor(), "f(${1:a}, ${2:b})")
	m.ghost = &ghostState{ed: e, pos: e.Buffer().Cursor(), from: e.Buffer().Cursor(), text: "SHOULDNOTAPPEAR", seq: e.Buffer().Seq()}
	if ghostShown(m) {
		t.Fatal("ghost shown during a snippet session")
	}
	before := lineAt(m, 0)
	feed(m, "tab")
	if lineAt(m, 0) != before || m.snip == nil {
		t.Fatalf("snippet tab must jump, not accept: %q", lineAt(m, 0))
	}
	// 3. no popup, no session: the ghost
	m.snip = nil
	e.Key("esc")
	e.Key("o")
	cur := e.Buffer().Cursor()
	m.ghost = &ghostState{ed: e, pos: cur, from: cur, text: "ghosted", seq: e.Buffer().Seq()}
	feed(m, "tab")
	if lineAt(m, cur.Line) != "ghosted" {
		t.Fatalf("ghost tab: %q", lineAt(m, cur.Line))
	}
	// 4. nothing: indent
	feed(m, "enter", "tab")
	if !strings.Contains(lineAt(m, cur.Line+1), "\t") {
		t.Fatalf("indent: %q", lineAt(m, cur.Line+1))
	}
}

// Clicks and selections address buffer columns; the ghost is skipped.
func TestGhostTextClickOnItLandsAtTheLineEnd(t *testing.T) {
	m, _ := ghostModel(t, "x := compute(\n", []map[string]any{plainItem("1, 2)")}, false, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	m.View()
	g := m.bufGeom
	row := strings.Split(plainView(m), "\n")[g.oy]
	x := len([]rune(row[:strings.Index(row, "1, 2)")])) + 1
	p, ok := m.bufferPos(x, g.oy)
	if !ok || p.Col > 13 {
		t.Fatalf("click on the ghost -> %+v ok=%v, want col <= 13", p, ok)
	}
}

// Wrapped lines keep the cursor in front of the ghost.
func TestGhostTextWithWrap(t *testing.T) {
	long := "x := compute(" + strings.Repeat("a", 90) + ","
	m, _ := ghostModel(t, long+"\n", []map[string]any{plainItem(" b)")}, true, true)
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	if v := plainView(m); !strings.Contains(v, " b)") {
		t.Fatalf("ghost lost with wrap:\n%s", v)
	}
	if c := m.active().Buffer().Cursor(); c.Col != len([]rune(long)) {
		t.Fatalf("cursor col %d", c.Col)
	}
}

func TestGhostTextGolden(t *testing.T) {
	m, _ := ghostModel(t, "package app\n\nfunc total() int {\n\treturn compute(\n}\n", []map[string]any{plainItem("1, 2)")}, false, true)
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 3, Col: 0})
	feed(m, "A")
	settle(t, m, func() bool { return ghostShown(m) && strings.Contains(plainView(m), "✗1") }, "ghost text and the diagnostic chip")
	golden.Snapshot(t, "editor_ghost_100x30", m.View())
}

// ---- :suggest ----

type fakeSuggest struct {
	mu    sync.Mutex
	calls int
	reqs  []runtime.SuggestRequest
	text  string
	err   error
	gate  chan struct{} // closed to release the call; nil = answer at once
	done  chan error    // the ctx error the call ended with
}

func (f *fakeSuggest) run(ctx context.Context, agent string, q runtime.SuggestRequest) (string, error) {
	f.mu.Lock()
	f.calls++
	f.reqs = append(f.reqs, q)
	gate, text, err := f.gate, f.text, f.err
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			if f.done != nil {
				f.done <- ctx.Err()
			}
			return "", ctx.Err()
		}
	}
	return text, err
}

func (f *fakeSuggest) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func suggestModel(t *testing.T, text string, fs *fakeSuggest) *Model {
	t.Helper()
	prevTick := suggestTickEvery
	suggestTickEvery = 10 * time.Millisecond
	t.Cleanup(func() { suggestTickEvery = prevTick })
	m, _ := ghostModel(t, text, nil, false, true)
	m.suggestHook = &suggestHook{agent: "scout", run: fs.run}
	return m
}

func TestSuggestChordShowsSpinnerThenGhostAndTabAccepts(t *testing.T) {
	fs := &fakeSuggest{text: "1, 2)", gate: make(chan struct{})}
	m := suggestModel(t, "x := compute(\n", fs)
	feed(m, "A")
	feed(m, `alt+\`)
	settle(t, m, func() bool {
		v := plainView(m)
		return strings.Contains(v, "suggesting with @scout") && strings.Contains(v, "✗1")
	}, "spinner and the diagnostic chip")
	if m.ghost != nil {
		t.Fatal("no ghost before the answer")
	}
	golden.Snapshot(t, "editor_suggest_pending_100x30", pinFrame(m))
	close(fs.gate)
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	if v := plainView(m); !strings.Contains(v, "x := compute( 1, 2)") || strings.Contains(v, "suggesting") {
		t.Fatalf("view:\n%s", v)
	}
	if !m.ghost.fromUser {
		t.Error("the ghost is the agent's")
	}
	feed(m, "tab")
	if lineAt(m, 0) != "x := compute(1, 2)" {
		t.Fatalf("line %q", lineAt(m, 0))
	}
}

// pinFrame draws with the spinner at frame 0 so the snapshot is stable.
func pinFrame(m *Model) string {
	if m.sug != nil {
		m.sug.frame = 0
	}
	return m.View()
}

func TestSuggestCommandFromNormalModeEntersInsertWithTheGhost(t *testing.T) {
	fs := &fakeSuggest{text: " b)"}
	m := suggestModel(t, "x := compute(a,\n", fs)
	feed(m, "$", ":")
	typeKeys(m, "suggest")
	feed(m, "enter")
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
	if m.active().Mode() != textbuf.ModeInsert {
		t.Fatal("the answer is shown in insert mode, where tab accepts")
	}
	feed(m, "tab")
	if lineAt(m, 0) != "x := compute(a, b)" {
		t.Fatalf("line %q", lineAt(m, 0))
	}
}

func TestSuggestRequestCarriesTheExcerptAroundTheCursor(t *testing.T) {
	fs := &fakeSuggest{text: "z"}
	m := suggestModel(t, "package app\n\nfunc f() {\n\treturn \n}\n", fs)
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 3, Col: 0})
	feed(m, "A", `alt+\`)
	settle(t, m, func() bool { return fs.count() == 1 }, "the call")
	fs.mu.Lock()
	q := fs.reqs[0]
	fs.mu.Unlock()
	if !strings.HasSuffix(q.Before, "func f() {\n\treturn ") || !strings.HasPrefix(q.After, "\n}") {
		t.Fatalf("excerpt: before=%q after=%q", q.Before, q.After)
	}
	if !strings.HasSuffix(q.Path, "app.go") || q.Language != "go" {
		t.Fatalf("request = %+v", q)
	}
}

// Never automatic: typing, pausing and moving make no agent call.
func TestSuggestIsNeverAutoTriggered(t *testing.T) {
	fs := &fakeSuggest{text: "z"}
	m := suggestModel(t, "x := 1\n", fs)
	feed(m, "A")
	typeKeys(m, " + compute(a, b)")
	feed(m, "esc", "o")
	typeKeys(m, "y := 2")
	for i := 0; i < 10; i++ {
		m.View()
		m.drainTerm()
		time.Sleep(10 * time.Millisecond)
	}
	if fs.count() != 0 {
		t.Fatalf("the agent was called %d times without being asked", fs.count())
	}
}

func TestSuggestEscCancelsThePendingCall(t *testing.T) {
	fs := &fakeSuggest{text: "z", gate: make(chan struct{}), done: make(chan error, 1)}
	m := suggestModel(t, "x := 1\n", fs)
	feed(m, "A", `alt+\`)
	settle(t, m, func() bool { return fs.count() == 1 }, "the call")
	feed(m, "esc")
	select {
	case err := <-fs.done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ctx error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("esc did not cancel the call")
	}
	if m.sug != nil || m.ghost != nil {
		t.Fatal("state left behind")
	}
}

func TestSuggestAnswerIsDroppedWhenTheCursorMoved(t *testing.T) {
	fs := &fakeSuggest{text: "z", gate: make(chan struct{})}
	m := suggestModel(t, "x := 1\ny := 2\n", fs)
	feed(m, "A", `alt+\`)
	settle(t, m, func() bool { return fs.count() == 1 }, "the call")
	feed(m, "q") // typing past it
	close(fs.gate)
	for i := 0; i < 10; i++ {
		m.View()
		m.drainTerm()
		time.Sleep(10 * time.Millisecond)
	}
	if m.ghost != nil || m.sug != nil {
		t.Fatalf("a stale answer was shown: %+v", m.ghost)
	}
}

func TestSuggestErrorsAreNamedAndShowNoGhost(t *testing.T) {
	fs := &fakeSuggest{err: errors.New("cursor-agent has no read-only mode (no verified way); nothing was started")}
	m := suggestModel(t, "x := 1\n", fs)
	feed(m, "A", `alt+\`)
	settle(t, m, func() bool { return strings.Contains(m.active().Message(), "no read-only mode") }, "the refusal")
	if m.ghost != nil {
		t.Fatal("a refused call must leave no ghost")
	}
}

func TestSuggestEmptyAnswerSaysSo(t *testing.T) {
	fs := &fakeSuggest{text: "  \n"}
	m := suggestModel(t, "x := 1\n", fs)
	feed(m, "A", `alt+\`)
	settle(t, m, func() bool { return strings.Contains(m.active().Message(), "nothing to add") }, "the note")
	if m.ghost != nil {
		t.Fatal("empty answer must show nothing")
	}
}

func TestSuggestNeedsTheEndOfALineAndAnAgent(t *testing.T) {
	fs := &fakeSuggest{text: "z"}
	m := suggestModel(t, "x := compute(1)\n", fs)
	feed(m, "0", "i", `alt+\`)
	if !strings.Contains(m.active().Message(), "end of a line") || fs.count() != 0 {
		t.Fatalf("message %q calls %d", m.active().Message(), fs.count())
	}
	// No agents: named, nothing runs.
	m2, _ := ghostModel(t, "x := 1\n", nil, false, true)
	feed(m2, "A", `alt+\`)
	if !strings.Contains(m2.active().Message(), "no agents yet") {
		t.Fatalf("message %q", m2.active().Message())
	}
}

func TestSuggestOnlyOneCallAtATime(t *testing.T) {
	fs := &fakeSuggest{text: "z", gate: make(chan struct{})}
	m := suggestModel(t, "x := 1\n", fs)
	feed(m, "A", `alt+\`)
	settle(t, m, func() bool { return fs.count() == 1 }, "the call")
	feed(m, `alt+\`)
	if fs.count() != 1 || !strings.Contains(m.active().Message(), "already asking") {
		t.Fatalf("calls %d message %q", fs.count(), m.active().Message())
	}
	close(fs.gate)
}

// :suggest works with the LSP setting off: they are separate.
func TestSuggestWorksWhenGhostTextIsOff(t *testing.T) {
	fs := &fakeSuggest{text: "z"}
	m := suggestModel(t, "x := 1\n", fs)
	m.prefs = func() Prefs { return Prefs{TabWidth: 4, LineNumbers: true, InlineCompletion: false, Scrollback: 1000} }
	feed(m, "A", `alt+\`)
	settle(t, m, func() bool { return ghostShown(m) }, "ghost text")
}
