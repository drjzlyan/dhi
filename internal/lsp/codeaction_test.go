package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodeActionOnlyAndResolve(t *testing.T) {
	var sawOnly, sawResolve bool
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		switch method {
		case "textDocument/codeAction":
			sawOnly = strings.Contains(string(params), `"only":["quickfix"]`)
			return []map[string]any{
				{"title": "off", "kind": "quickfix", "disabled": map[string]any{"reason": "nope"}},
				{"title": "lazy", "kind": "quickfix", "isPreferred": true, "data": map[string]any{"k": 1}},
			}, true
		case "codeAction/resolve":
			sawResolve = strings.Contains(string(params), `"data":{"k":1}`)
			return map[string]any{"title": "lazy", "edit": map[string]any{"changes": map[string]any{
				"file:///ws/a.go": []map[string]any{{"range": map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 0}}, "newText": "x"}},
			}}}, true
		}
		return nil, false
	})
	acts, err := c.CodeActionOnly("/ws/a.go", Range{}, nil, []string{"quickfix"})
	if err != nil || len(acts) != 2 || !sawOnly {
		t.Fatalf("acts=%+v err=%v only=%v", acts, err, sawOnly)
	}
	if acts[0].Disabled != "nope" || !acts[1].Preferred {
		t.Errorf("flags = %+v", acts)
	}
	r, err := c.ResolveCodeAction(acts[1])
	if err != nil || r.Edit == nil || !sawResolve {
		t.Fatalf("resolved = %+v err=%v saw=%v", r, err, sawResolve)
	}
}
