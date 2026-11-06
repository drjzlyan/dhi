package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSignatureHelpShapes(t *testing.T) {
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		if method != "textDocument/signatureHelp" {
			return nil, false
		}
		switch {
		case strings.Contains(string(params), `"line":7`):
			return nil, true // null → nil, nil
		case strings.Contains(string(params), `"line":8`):
			return map[string]any{"signatures": []any{}}, true
		case strings.Contains(string(params), `"line":1`):
			// Label offsets are UTF-16 into the label; the signature's own
			// activeParameter beats the response's; activeSignature picks.
			return map[string]any{
				"signatures": []any{
					map[string]any{"label": "other()"},
					map[string]any{
						"label":           "f(é string, b int)",
						"parameters":      []any{map[string]any{"label": []int{2, 10}}, map[string]any{"label": []int{12, 17}}},
						"activeParameter": 1,
						"documentation":   map[string]any{"kind": "markdown", "value": "doc"},
					},
				},
				"activeSignature": 1, "activeParameter": 0,
			}, true
		}
		return map[string]any{
			"signatures": []any{map[string]any{
				"label":      "Add(a int, b int)",
				"parameters": []any{map[string]any{"label": "a int"}, map[string]any{"label": "b int"}},
			}},
			"activeParameter": 1,
		}, true
	})
	h, err := c.SignatureHelp("/ws/a.go", 0, 4)
	if err != nil || h == nil {
		t.Fatalf("help = %+v err = %v", h, err)
	}
	if h.Label != "Add(a int, b int)" || h.ParamStart != 11 || h.ParamEnd != 16 {
		t.Errorf("substring label: %+v", h)
	}
	h, _ = c.SignatureHelp("/ws/a.go", 1, 0)
	if h == nil || h.Label != "f(é string, b int)" || h.ParamStart != 12 || h.ParamEnd != 17 || h.Doc != "doc" {
		t.Errorf("offset label / active signature: %+v", h)
	}
	for _, line := range []int{7, 8} {
		if h, err := c.SignatureHelp("/ws/a.go", line, 0); h != nil || err != nil {
			t.Errorf("line %d: help = %+v err = %v, want nil, nil", line, h, err)
		}
	}
}

func TestSignatureTriggers(t *testing.T) {
	c := &Client{}
	if got := c.SignatureTriggers(); len(got) != 2 {
		t.Errorf("no capabilities reported: %v, want the defaults", got)
	}
	c.caps = map[string]json.RawMessage{"hoverProvider": json.RawMessage(`true`)}
	if got := c.SignatureTriggers(); len(got) != 0 {
		t.Errorf("server without signatureHelpProvider: %v, want none", got)
	}
	c.caps["signatureHelpProvider"] = json.RawMessage(`{"triggerCharacters":["("],"retriggerCharacters":[","]}`)
	if got := c.SignatureTriggers(); strings.Join(got, "") != "(," {
		t.Errorf("advertised triggers: %v", got)
	}
}
