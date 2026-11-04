package lsp

import (
	"encoding/json"
	"testing"
)

func TestCanInlayHintsNeedsTheCapability(t *testing.T) {
	c := &Client{}
	if c.CanInlayHints() {
		t.Fatal("a server that reported nothing must not be asked")
	}
	c.caps = map[string]json.RawMessage{"inlayHintProvider": json.RawMessage(`false`)}
	if c.CanInlayHints() {
		t.Fatal("inlayHintProvider:false")
	}
	c.caps["inlayHintProvider"] = json.RawMessage(`true`)
	if !c.CanInlayHints() {
		t.Fatal("inlayHintProvider:true")
	}
	c.caps["inlayHintProvider"] = json.RawMessage(`{"resolveProvider":true}`)
	if !c.CanInlayHints() {
		t.Fatal("inlayHintProvider options object")
	}
}

func TestInlayHintsDecodeStringAndPartLabels(t *testing.T) {
	var asked json.RawMessage
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		if method != "textDocument/inlayHint" {
			return nil, false
		}
		asked = params
		return []map[string]any{
			{"position": map[string]any{"line": 1, "character": 3}, "label": ": int", "kind": 1},
			{"position": map[string]any{"line": 2, "character": 7}, "kind": 2, "paddingRight": true,
				"label": []map[string]any{{"value": "na"}, {"value": "me:"}}},
			{"position": map[string]any{"line": 2, "character": 9}, "label": ""},
		}, true
	})
	hs, err := c.InlayHints("/ws/a.go", Range{Start: Position{Line: 1}, End: Position{Line: 4, Character: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 {
		t.Fatalf("hints = %+v (empty label must be dropped)", hs)
	}
	if hs[0] != (InlayHint{Line: 1, Col: 3, Label: ": int", Kind: InlayType}) {
		t.Errorf("type hint = %+v", hs[0])
	}
	if hs[1] != (InlayHint{Line: 2, Col: 7, Label: "name:", Kind: InlayParameter, PadRight: true}) {
		t.Errorf("parameter hint = %+v", hs[1])
	}
	var p struct {
		Range struct {
			Start, End struct{ Line, Character int }
		} `json:"range"`
	}
	if err := json.Unmarshal(asked, &p); err != nil || p.Range.End.Line != 4 || p.Range.End.Character != 2 {
		t.Errorf("range sent = %s", asked)
	}
}

func TestInlayHintsNullResult(t *testing.T) {
	c := startFake(t, nil)
	hs, err := c.InlayHints("/ws/a.go", Range{})
	if err != nil || len(hs) != 0 {
		t.Fatalf("null result = %+v err=%v", hs, err)
	}
}

// Only gopls needs telling; its options answer workspace/configuration.
func TestSetInlayHintsConfiguresGopls(t *testing.T) {
	c := startFake(t, nil)
	if err := c.SetInlayHints(true); err != nil {
		t.Fatal(err)
	}
	if got := c.configuration(json.RawMessage(`{"items":[{"section":"gopls"}]}`)); len(got) != 1 || got[0] == nil {
		t.Fatalf("other servers must be left alone, got %v", got)
	}
	c.serverName = "gopls"
	if err := c.SetInlayHints(true); err != nil {
		t.Fatal(err)
	}
	got := c.configuration(json.RawMessage(`{"items":[{"section":"gopls"},{"section":"other"}]}`))
	b, _ := json.Marshal(got)
	const want = `[{"hints":{"assignVariableTypes":true,"compositeLiteralFields":true,"functionTypeParameters":true,"parameterNames":true,"rangeVariableTypes":true}},{}]`
	if string(b) != want {
		t.Fatalf("configuration = %s\nwant %s", b, want)
	}
	if err := c.SetInlayHints(false); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(c.configuration(json.RawMessage(`{"items":[{"section":"gopls"}]}`)))
	if string(b) != `[{"hints":{"assignVariableTypes":false,"compositeLiteralFields":false,"functionTypeParameters":false,"parameterNames":false,"rangeVariableTypes":false}}]` {
		t.Fatalf("off = %s", b)
	}
}
