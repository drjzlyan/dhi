package lsp

import "strings"

// CanSelectionRange reports whether the server advertises
// selectionRangeProvider. Like CanFormat, a server that reports no
// capabilities at all (test doubles) is given the benefit of the doubt;
// one that reports some and omits it is not asked (ADR-0011).
func (c *Client) CanSelectionRange() bool {
	if c.caps == nil {
		return true
	}
	v, ok := c.caps["selectionRangeProvider"]
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(v))
	return s != "false" && s != "null" && s != ""
}

// SelectionRanges asks for the nested syntax ranges around a position
// (textDocument/selectionRange, F-167) and flattens the parent chain,
// innermost first. Nil when the server has none. Columns are UTF-16 units.
func (c *Client) SelectionRanges(path string, line, col int) ([]Range, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"positions":    []map[string]any{{"line": line, "character": col}},
	}
	type sel struct {
		Range  Range `json:"range"`
		Parent *sel  `json:"parent"`
	}
	var res []*sel
	if err := c.call("textDocument/selectionRange", params, &res); err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	var out []Range
	for s := res[0]; s != nil; s = s.Parent {
		out = append(out, s.Range)
	}
	return out, nil
}
