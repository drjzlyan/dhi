package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func rng(l1, c1, l2, c2 int) map[string]any {
	return map[string]any{
		"start": map[string]any{"line": l1, "character": c1},
		"end":   map[string]any{"line": l2, "character": c2},
	}
}

func TestSelectionRangesFlattensParents(t *testing.T) {
	var saw string
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		if method != "textDocument/selectionRange" {
			return nil, false
		}
		saw = string(params)
		return []map[string]any{{
			"range": rng(0, 4, 0, 7),
			"parent": map[string]any{
				"range":  rng(0, 2, 0, 9),
				"parent": map[string]any{"range": rng(0, 0, 3, 1)},
			},
		}}, true
	})
	got, err := c.SelectionRanges("/ws/a.go", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != (Range{Position{0, 4}, Position{0, 7}}) || got[2].End != (Position{3, 1}) {
		t.Fatalf("chain = %+v", got)
	}
	if !strings.Contains(saw, `"positions":[{"character":5,"line":0}]`) || !strings.Contains(saw, "file:///ws/a.go") {
		t.Errorf("request = %s", saw)
	}
}

func TestSelectionRangesNull(t *testing.T) {
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		if method == "textDocument/selectionRange" {
			return nil, true
		}
		return nil, false
	})
	got, err := c.SelectionRanges("/ws/a.go", 0, 0)
	if err != nil || got != nil {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestCanSelectionRange(t *testing.T) {
	cases := []struct {
		name string
		caps map[string]json.RawMessage
		want bool
	}{
		{"no caps reported (test double)", nil, true},
		{"advertised true", map[string]json.RawMessage{"selectionRangeProvider": json.RawMessage(`true`)}, true},
		{"advertised options", map[string]json.RawMessage{"selectionRangeProvider": json.RawMessage(`{"workDoneProgress":true}`)}, true},
		{"advertised false", map[string]json.RawMessage{"selectionRangeProvider": json.RawMessage(`false`)}, false},
		{"other caps only", map[string]json.RawMessage{"hoverProvider": json.RawMessage(`true`)}, false},
	}
	for _, tc := range cases {
		c := &Client{caps: tc.caps}
		if got := c.CanSelectionRange(); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
