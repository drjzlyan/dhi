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
