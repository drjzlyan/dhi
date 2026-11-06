package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Folding ranges (F-176): the spans a server says can be folded.

// FoldingRange is one foldable span in whole lines (0-based, inclusive).
type FoldingRange struct {
	StartLine, EndLine int
	Kind               string // "comment", "imports", "region" or ""
}

// CanFoldingRange reports whether the server advertises foldingRangeProvider.
// A server that reports nothing is not asked (ADR-0011: no guessing).
func (c *Client) CanFoldingRange() bool {
	v, ok := c.caps["foldingRangeProvider"]
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(v))
	return s != "false" && s != "null" && s != ""
}

// FoldingRanges asks for every foldable span of path.
func (c *Client) FoldingRanges(path string) ([]FoldingRange, error) {
	var raw json.RawMessage
	params := map[string]any{"textDocument": map[string]any{"uri": pathToURI(path)}}
	if err := c.call("textDocument/foldingRange", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []struct {
		StartLine int    `json:"startLine"`
		EndLine   int    `json:"endLine"`
		Kind      string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("lsp: foldingRange result: %w", err)
	}
	out := make([]FoldingRange, 0, len(items))
	for _, it := range items {
		out = append(out, FoldingRange{StartLine: it.StartLine, EndLine: it.EndLine, Kind: it.Kind})
	}
	return out, nil
}
