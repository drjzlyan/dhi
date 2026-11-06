package lsp

import (
	"encoding/json"
	"fmt"
)

// SemanticTypes and SemanticModifiers are the legend the client
// advertises (F-173): the standard names the editor can colour. A server
// answers in its own legend, which SemanticTokens decodes.
var (
	SemanticTypes = []string{"namespace", "type", "class", "enum", "interface", "struct",
		"typeParameter", "parameter", "variable", "property", "function", "method"}
	SemanticModifiers = []string{"readonly", "declaration", "definition", "static"}
)

// SemanticToken is one classified span on a line. Col and Len count UTF-16
// units, as on the wire; callers convert with RuneCol.
type SemanticToken struct {
	Line, Col, Len int
	Type           string
	Modifiers      []string
}

// Has reports whether the token carries modifier mod.
func (t SemanticToken) Has(mod string) bool {
	for _, m := range t.Modifiers {
		if m == mod {
			return true
		}
	}
	return false
}

// semLegend is the server's token legend.
type semLegend struct {
	Types     []string `json:"tokenTypes"`
	Modifiers []string `json:"tokenModifiers"`
}

type semProvider struct {
	Legend semLegend       `json:"legend"`
	Full   json.RawMessage `json:"full"`
}

// CanSemanticTokens reports whether the server offers full-document
// semantic tokens.
func (c *Client) CanSemanticTokens() bool {
	if c.caps == nil {
		return true
	}
	if !c.supports("semanticTokensProvider") {
		return false
	}
	var p semProvider
	if json.Unmarshal(c.caps["semanticTokensProvider"], &p) != nil {
		return false
	}
	f := string(p.Full)
	return len(p.Legend.Types) > 0 && f != "" && f != "false" && f != "null"
}

// SemanticTokens requests the whole document's tokens and decodes them
// with the server's legend.
func (c *Client) SemanticTokens(path string) ([]SemanticToken, error) {
	var p semProvider
	if raw, ok := c.caps["semanticTokensProvider"]; ok {
		_ = json.Unmarshal(raw, &p)
	}
	var res struct {
		Data []int `json:"data"`
	}
	params := map[string]any{"textDocument": map[string]any{"uri": pathToURI(path)}}
	if err := c.call("textDocument/semanticTokens/full", params, &res); err != nil {
		return nil, err
	}
	toks, err := DecodeSemanticTokens(res.Data, p.Legend.Types, p.Legend.Modifiers)
	if err != nil {
		return nil, fmt.Errorf("lsp: semanticTokens result: %w", err)
	}
	return toks, nil
}

// DecodeSemanticTokens expands the relative 5-integer encoding
// (deltaLine, deltaStart, length, type, modifier bits).
func DecodeSemanticTokens(data []int, types, mods []string) ([]SemanticToken, error) {
	if len(data)%5 != 0 {
		return nil, fmt.Errorf("data length %d is not a multiple of 5", len(data))
	}
	out := make([]SemanticToken, 0, len(data)/5)
	line, col := 0, 0
	for i := 0; i < len(data); i += 5 {
		dl, ds, n, ty, bits := data[i], data[i+1], data[i+2], data[i+3], data[i+4]
		if dl > 0 {
			line, col = line+dl, ds
		} else {
			col += ds
		}
		if ty < 0 || ty >= len(types) {
			continue // a type outside the legend: nothing to say about it
		}
		t := SemanticToken{Line: line, Col: col, Len: n, Type: types[ty]}
		for b := 0; b < len(mods); b++ {
			if bits&(1<<b) != 0 {
				t.Modifiers = append(t.Modifiers, mods[b])
			}
		}
		out = append(out, t)
	}
	return out, nil
}
