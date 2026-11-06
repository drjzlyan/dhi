package lsp

import (
	"encoding/json"
	"testing"
)

func TestCanFoldingRangeNeedsTheCapability(t *testing.T) {
	c := &Client{}
	if c.CanFoldingRange() {
		t.Fatal("a server that reported nothing must not be asked")
	}
	c.caps = map[string]json.RawMessage{"foldingRangeProvider": json.RawMessage(`false`)}
	if c.CanFoldingRange() {
		t.Fatal("foldingRangeProvider:false")
	}
	for _, v := range []string{`true`, `{"workDoneProgress":true}`} {
		c.caps["foldingRangeProvider"] = json.RawMessage(v)
		if !c.CanFoldingRange() {
			t.Fatalf("foldingRangeProvider:%s", v)
		}
	}
}

func TestFoldingRangesDecode(t *testing.T) {
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		if method != "textDocument/foldingRange" {
			return nil, false
		}
		return []map[string]any{
			{"startLine": 2, "endLine": 9},
			{"startLine": 0, "endLine": 1, "kind": "comment"},
		}, true
	})
	rs, err := c.FoldingRanges("/ws/a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0] != (FoldingRange{StartLine: 2, EndLine: 9}) || rs[1].Kind != "comment" {
		t.Errorf("ranges = %+v", rs)
	}
}
