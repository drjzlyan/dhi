package dhitools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeEditor struct {
	opened   []string
	revealed string
	applied  [4]string
	lspOp    string
	lspPath  string
	lspLine  int
	lspCol   int
	lspArg   string
	lspText  string
	err      error
}

func (f *fakeEditor) Open(_ context.Context, paths []string) error {
	f.opened = append(f.opened, paths...)
	return f.err
}
func (f *fakeEditor) Reveal(_ context.Context, path string) error {
	f.revealed = path
	return f.err
}
func (f *fakeEditor) Apply(_ context.Context, path, old, new string, _ bool) error {
	f.applied = [4]string{path, old, new, ""}
	return f.err
}
func (f *fakeEditor) LSP(_ context.Context, op, path string, line, col int, arg string) (string, error) {
	f.lspOp, f.lspPath, f.lspLine, f.lspCol, f.lspArg = op, path, line, col, arg
	return f.lspText, f.err
}

func TestEditorToolsServed(t *testing.T) {
	for _, s := range []string{"editor_open", "editor_reveal"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}

func TestEditorOpenRoutesThroughSeam(t *testing.T) {
	f, m := newFixture(t, "editor_open")
	fe := &fakeEditor{}
	h := Deps{Agent: m, WS: f.ws, Editor: fe, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "editor_open", `{"paths":["api/main.go","api/pkg/util.go"]}`)
	if isErr {
		t.Fatalf("editor_open refused: %s", out)
	}
	if strings.Join(fe.opened, ",") != "api/main.go,api/pkg/util.go" {
		t.Fatalf("opened = %v", fe.opened)
	}
}

func TestEditorRevealRoutesThroughSeam(t *testing.T) {
	f, m := newFixture(t, "editor_reveal")
	fe := &fakeEditor{}
	h := Deps{Agent: m, WS: f.ws, Editor: fe, Approvals: f.approvals}.Handler()
	if out, isErr := call(h, t, "editor_reveal", `{"path":"api/main.go"}`); isErr {
		t.Fatalf("editor_reveal refused: %s", out)
	}
	if fe.revealed != "api/main.go" {
		t.Fatalf("revealed = %q", fe.revealed)
	}
}

func TestEditorToolsRefuseWithoutSeam(t *testing.T) {
	f, m := newFixture(t, "editor_open")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "editor_open", `{"paths":["api/main.go"]}`)
	if !isErr || !strings.Contains(out, "editor unavailable") {
		t.Fatalf("no-seam open = %q isErr=%v", out, isErr)
	}
}

func TestEditorToolsRefuseEmptyArgs(t *testing.T) {
	f, m := newFixture(t, "editor_open")
	h := Deps{Agent: m, WS: f.ws, Editor: &fakeEditor{}, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "editor_open", `{"paths":[]}`)
	if !isErr || !strings.Contains(out, "path") {
		t.Fatalf("empty open = %q isErr=%v", out, isErr)
	}
}

func TestEditorApplyEditRoutesThroughSeam(t *testing.T) {
	f, m := newFixture(t, "editor_apply_edit")
	fe := &fakeEditor{}
	h := Deps{Agent: m, WS: f.ws, Editor: fe, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "editor_apply_edit", `{"path":"api/main.go","old":"a","new":"b"}`, f)
	if out, isErr := resolve(t); isErr {
		t.Fatalf("apply refused: %s", out)
	}
	if fe.applied[0] != "api/main.go" || fe.applied[1] != "a" || fe.applied[2] != "b" {
		t.Fatalf("applied = %v", fe.applied)
	}
}

func TestEditorApplyEditRefusesEmptyOldBeforeApproval(t *testing.T) {
	f, m := newFixture(t, "editor_apply_edit")
	h := Deps{Agent: m, WS: f.ws, Editor: &fakeEditor{}, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "editor_apply_edit", `{"path":"api/main.go","old":"","new":"b"}`)
	if !isErr || !strings.Contains(out, "old text") {
		t.Fatalf("empty old = %q isErr=%v", out, isErr)
	}
	if len(f.approvals.List()) != 0 {
		t.Fatal("malformed apply parked an approval")
	}
}

func TestEditorSeamErrorSurfaces(t *testing.T) {
	f, m := newFixture(t, "editor_reveal")
	h := Deps{Agent: m, WS: f.ws, Editor: &fakeEditor{err: errors.New("editor busy")}, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "editor_reveal", `{"path":"api/main.go"}`)
	if !isErr || !strings.Contains(out, "editor busy") {
		t.Fatalf("seam error = %q isErr=%v", out, isErr)
	}
}

func TestLSPToolsServed(t *testing.T) {
	for _, s := range []string{"lsp_hover", "lsp_definition", "lsp_references", "lsp_rename", "lsp_code_action"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}

func TestLSPHoverRoutesThroughSeam(t *testing.T) {
	f, m := newFixture(t, "lsp_hover")
	fe := &fakeEditor{lspText: "func foo()"}
	h := Deps{Agent: m, WS: f.ws, Editor: fe, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "lsp_hover", `{"path":"api/main.go","line":3,"col":5}`)
	if isErr || !strings.Contains(out, "func foo") {
		t.Fatalf("hover = %q isErr=%v", out, isErr)
	}
	if fe.lspOp != "hover" || fe.lspPath != "api/main.go" || fe.lspLine != 3 || fe.lspCol != 5 {
		t.Fatalf("seam got op=%q path=%q line=%d col=%d", fe.lspOp, fe.lspPath, fe.lspLine, fe.lspCol)
	}
}

func TestLSPRenameRequiresNewNameBeforeApproval(t *testing.T) {
	f, m := newFixture(t, "lsp_rename")
	h := Deps{Agent: m, WS: f.ws, Editor: &fakeEditor{}, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "lsp_rename", `{"path":"api/main.go","line":1,"col":1}`)
	if !isErr || !strings.Contains(out, "new_name") {
		t.Fatalf("rename no-arg = %q isErr=%v", out, isErr)
	}
	if len(f.approvals.List()) != 0 {
		t.Fatal("malformed rename parked an approval")
	}
}

func TestLSPRenameRoutesWithApproval(t *testing.T) {
	f, m := newFixture(t, "lsp_rename")
	fe := &fakeEditor{lspText: "renamed across 2 file(s)"}
	h := Deps{Agent: m, WS: f.ws, Editor: fe, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "lsp_rename", `{"path":"api/main.go","line":1,"col":1,"new_name":"Bar"}`, f)
	if out, isErr := resolve(t); isErr || !strings.Contains(out, "renamed") {
		t.Fatalf("rename = %q isErr=%v", out, isErr)
	}
	if fe.lspOp != "rename" || fe.lspArg != "Bar" {
		t.Fatalf("seam got op=%q arg=%q", fe.lspOp, fe.lspArg)
	}
}

func TestLSPToolsRefuseWithoutSeam(t *testing.T) {
	f, m := newFixture(t, "lsp_hover")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "lsp_hover", `{"path":"api/main.go","line":0,"col":0}`)
	if !isErr || !strings.Contains(out, "editor unavailable") {
		t.Fatalf("no-seam hover = %q isErr=%v", out, isErr)
	}
}

// fakePair adds the PairAPI (F-038) to fakeEditor.
type fakePair struct {
	fakeEditor
	ctxText  string
	proposed [5]string
}

func (f *fakePair) Context(context.Context) (string, error) { return f.ctxText, f.err }
func (f *fakePair) Propose(_ context.Context, path, old, new, note, from string) error {
	f.proposed = [5]string{path, old, new, note, from}
	return f.err
}

func TestPairToolsServedAndRoute(t *testing.T) {
	for _, s := range []string{"editor_context", "editor_propose_edit"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
	f, m := newFixture(t, "editor_context", "editor_propose_edit")
	fp := &fakePair{ctxText: "file: api/main.go\ncursor: line 3 col 0"}
	h := Deps{Agent: m, WS: f.ws, Editor: fp, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "editor_context", `{}`)
	if isErr || !strings.Contains(out, "cursor: line 3") {
		t.Fatalf("context = %q isErr=%v", out, isErr)
	}
	// A proposal does not mutate, so it must not park in the approvals
	// queue: the human's accept/reject in the editor is the gate.
	out, isErr = call(h, t, "editor_propose_edit", `{"path":"api/main.go","old":"a","new":"b","note":"why"}`)
	if isErr {
		t.Fatalf("propose refused: %s", out)
	}
	if fp.proposed != [5]string{"api/main.go", "a", "b", "why", "scout"} {
		t.Fatalf("proposed = %v", fp.proposed)
	}
	if n := len(f.approvals.List()); n != 0 {
		t.Fatalf("propose must not enqueue approvals, got %d", n)
	}
}

func TestPairToolsRefuseWithoutPairAPI(t *testing.T) {
	f, m := newFixture(t, "editor_context", "editor_propose_edit")
	// A plain EditorAPI (no PairAPI) refuses by name.
	h := Deps{Agent: m, WS: f.ws, Editor: &fakeEditor{}, Approvals: f.approvals}.Handler()
	if out, isErr := call(h, t, "editor_context", `{}`); !isErr || !strings.Contains(out, "pair programming") {
		t.Fatalf("context = %q isErr=%v", out, isErr)
	}
	// No editor surface at all also refuses.
	h = Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	if out, isErr := call(h, t, "editor_propose_edit", `{"path":"api/x","old":"a","new":"b"}`); !isErr || !strings.Contains(out, "editor unavailable") {
		t.Fatalf("propose = %q isErr=%v", out, isErr)
	}
	// Bad args are strict.
	h = Deps{Agent: m, WS: f.ws, Editor: &fakePair{}, Approvals: f.approvals}.Handler()
	if _, isErr := call(h, t, "editor_propose_edit", `{"path":"api/x","old":"","new":"b"}`); !isErr {
		t.Fatal("empty old must refuse")
	}
	if _, isErr := call(h, t, "editor_propose_edit", `{"path":"api/x","old":"a","new":"b","bogus":1}`); !isErr {
		t.Fatal("unknown arg must refuse")
	}
}

type fakeDebug struct {
	fakeEditor
	state string
}

func (f *fakeDebug) DebugState(context.Context) (string, error) { return f.state, f.err }

func TestDebugStateToolRoutesAndRefuses(t *testing.T) {
	if !Serves("debug_state") {
		t.Fatal("debug_state is not a served slug")
	}
	f, m := newFixture(t, "debug_state")
	fd := &fakeDebug{state: "stopped: breakpoint\n#0 main.run api/main.go:10"}
	h := Deps{Agent: m, WS: f.ws, Editor: fd, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "debug_state", `{}`)
	if isErr || !strings.Contains(out, "stopped: breakpoint") {
		t.Fatalf("debug_state = %q isErr=%v", out, isErr)
	}
	if n := len(f.approvals.List()); n != 0 {
		t.Fatalf("read-only tool must not enqueue approvals, got %d", n)
	}
	// An editor without the debugger extension refuses by name.
	h = Deps{Agent: m, WS: f.ws, Editor: &fakeEditor{}, Approvals: f.approvals}.Handler()
	if out, isErr := call(h, t, "debug_state", `{}`); !isErr || !strings.Contains(out, "debugger") {
		t.Fatalf("no-debug = %q isErr=%v", out, isErr)
	}
}
