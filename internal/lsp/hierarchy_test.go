package lsp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeSemanticTokensRelativeEncoding(t *testing.T) {
	types := []string{"namespace", "function", "variable"}
	mods := []string{"readonly", "static"}
	// line 0 col 8 len 4 namespace; same line +5 len 3 variable readonly+static;
	// line 2 col 1 len 2 function; an unknown type (9) is skipped but still moves the cursor.
	data := []int{0, 8, 4, 0, 0, 0, 5, 3, 2, 3, 2, 1, 2, 1, 0, 0, 4, 9, 0}
	if _, err := DecodeSemanticTokens(data, types, mods); err == nil {
		t.Fatal("a length that is not a multiple of five must be an error")
	}
	data = []int{0, 8, 4, 0, 0, 0, 5, 3, 2, 3, 2, 1, 2, 1, 0, 0, 0, 4, 9, 0}
	got, err := DecodeSemanticTokens(data, types, mods)
	if err != nil {
		t.Fatal(err)
	}
	want := []SemanticToken{
		{Line: 0, Col: 8, Len: 4, Type: "namespace"},
		{Line: 0, Col: 13, Len: 3, Type: "variable", Modifiers: []string{"readonly", "static"}},
		{Line: 2, Col: 1, Len: 2, Type: "function"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !got[1].Has("readonly") || got[2].Has("readonly") {
		t.Fatal("Has misreports modifiers")
	}
}

func TestCapabilityGates(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	if c := (&Client{}); !c.CanCallHierarchy() || !c.CanSemanticTokens() {
		t.Error("a server that reports no capabilities gets the benefit of the doubt")
	}
	c := &Client{caps: map[string]json.RawMessage{"hoverProvider": raw("true")}}
	if c.CanCallHierarchy() || c.CanSemanticTokens() {
		t.Error("capabilities reported without the feature must disable it")
	}
	c = &Client{caps: map[string]json.RawMessage{
		"callHierarchyProvider":  raw("true"),
		"semanticTokensProvider": raw(`{"legend":{"tokenTypes":["type"],"tokenModifiers":[]},"full":{"delta":true}}`),
	}}
	if !c.CanCallHierarchy() || !c.CanSemanticTokens() {
		t.Error("advertised features must be on")
	}
	c = &Client{caps: map[string]json.RawMessage{"semanticTokensProvider": raw(`{"legend":{"tokenTypes":["type"],"tokenModifiers":[]},"range":true}`)}}
	if c.CanSemanticTokens() {
		t.Error("a range-only provider has no full request")
	}
}

func TestCallHierarchyRoundTripsTheServerItem(t *testing.T) {
	item := `{"name":"Add","kind":12,"detail":"pkg","uri":"file:///w/a.go","data":{"k":1},` +
		`"range":{"start":{"line":3,"character":0},"end":{"line":5,"character":1}},` +
		`"selectionRange":{"start":{"line":3,"character":5},"end":{"line":3,"character":8}}}`
	var sentItem string
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		switch method {
		case "textDocument/prepareCallHierarchy":
			return []json.RawMessage{json.RawMessage(item)}, true
		case "callHierarchy/incomingCalls":
			var p struct {
				Item json.RawMessage `json:"item"`
			}
			json.Unmarshal(params, &p)
			sentItem = string(p.Item)
			return []map[string]any{{"from": json.RawMessage(item), "fromRanges": []any{
				map[string]any{"start": map[string]any{"line": 9, "character": 1}, "end": map[string]any{"line": 9, "character": 4}}}}}, true
		}
		return nil, false
	})
	items, err := c.PrepareCallHierarchy("/w/a.go", 3, 5)
	if err != nil || len(items) != 1 {
		t.Fatalf("prepare: %v %v", items, err)
	}
	it := items[0]
	if it.Name != "Add" || it.Kind == "" || it.Select.Start.Character != 5 || it.PathFor() != "/w/a.go" {
		t.Fatalf("decoded item %+v", it)
	}
	in, err := c.IncomingCalls(it)
	if err != nil || len(in) != 1 || len(in[0].Sites) != 1 || in[0].Sites[0].Start.Line != 9 {
		t.Fatalf("incoming: %+v %v", in, err)
	}
	if !strings.Contains(sentItem, `"data":{"k":1}`) {
		t.Fatalf("the server's opaque data must come back unchanged: %s", sentItem)
	}
}

func TestServerInitOptions(t *testing.T) {
	if o := serverInitOptions("/opt/homebrew/bin/gopls"); o["semanticTokens"] != true {
		t.Fatalf("gopls must be asked for semantic tokens: %v", o)
	}
	if o := serverInitOptions("/usr/bin/pyright-langserver"); o != nil {
		t.Fatalf("other servers get no options: %v", o)
	}
}
