package lsp

import (
	"encoding/json"
	"testing"
)

func TestCanInlineCompletionNeedsTheCapability(t *testing.T) {
	c := &Client{}
	if c.CanInlineCompletion() {
		t.Fatal("a server that reported nothing must not be asked")
	}
	c.caps = map[string]json.RawMessage{"inlineCompletionProvider": json.RawMessage(`false`)}
	if c.CanInlineCompletion() {
		t.Fatal("inlineCompletionProvider:false")
	}
	c.caps["inlineCompletionProvider"] = json.RawMessage(`true`)
	if !c.CanInlineCompletion() {
		t.Fatal("inlineCompletionProvider:true")
	}
	c.caps["inlineCompletionProvider"] = json.RawMessage(`{"workDoneProgress":true}`)
	if !c.CanInlineCompletion() {
		t.Fatal("inlineCompletionProvider options object")
	}
}

func TestInlineCompletionDecodesStringSnippetAndListShapes(t *testing.T) {
	var asked json.RawMessage
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		if method != "textDocument/inlineCompletion" {
			return nil, false
		}
		asked = params
		return map[string]any{"items": []map[string]any{
			{"insertText": "foo(bar)"},
			{"insertText": map[string]any{"kind": "snippet", "value": "if ${1:cond} {\n\t$0\n}"},
				"range": map[string]any{"start": map[string]any{"line": 2, "character": 1}, "end": map[string]any{"line": 2, "character": 3}}},
			{"insertText": ""},
		}}, true
	})
	items, err := c.InlineCompletion("/ws/a.go", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v (empty text must be dropped)", items)
	}
	if items[0].Text != "foo(bar)" || items[0].Snippet || items[0].Range != nil {
		t.Errorf("plain item = %+v", items[0])
	}
	if !items[1].Snippet || items[1].Range == nil || items[1].Range.Start.Character != 1 {
		t.Errorf("snippet item = %+v", items[1])
	}
	var p struct {
		Position struct{ Line, Character int }
		Context  struct{ TriggerKind int }
	}
	if err := json.Unmarshal(asked, &p); err != nil || p.Position.Line != 2 || p.Position.Character != 3 || p.Context.TriggerKind != 2 {
		t.Errorf("params sent = %s", asked)
	}
}

func TestDecodeInlineNullAndBareArray(t *testing.T) {
	if items, err := decodeInline(json.RawMessage(`null`)); err != nil || items != nil {
		t.Fatalf("null = %v %v", items, err)
	}
	items, err := decodeInline(json.RawMessage(`[{"insertText":"x"}]`))
	if err != nil || len(items) != 1 || items[0].Text != "x" {
		t.Fatalf("array = %v %v", items, err)
	}
	if _, err := decodeInline(json.RawMessage(`42`)); err == nil {
		t.Fatal("a malformed result is an error, not silence")
	}
}
