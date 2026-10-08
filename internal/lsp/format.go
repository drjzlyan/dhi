package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Formatting asks the server to format a whole document and returns the
// edits to apply (nil when the document is already formatted).
func (c *Client) Formatting(path string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"options":      map[string]any{"tabSize": tabSize, "insertSpaces": insertSpaces},
	}
	if err := c.call("textDocument/formatting", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var edits []TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil, fmt.Errorf("lsp: formatting result: %w", err)
	}
	return edits, nil
}

// Symbol is one entry of a document outline.
type Symbol struct {
	Name      string
	Kind      string
	Line, Col int // 0-based start of the symbol's name
	Depth     int // nesting depth in a hierarchical outline
}

// symbolKinds names the LSP SymbolKind numbers worth showing.
var symbolKinds = map[int]string{
	2: "module", 3: "namespace", 5: "class", 6: "method", 7: "property",
	8: "field", 9: "constructor", 10: "enum", 11: "interface",
	12: "func", 13: "var", 14: "const", 22: "member", 23: "struct",
	25: "operator", 26: "type",
}

func symbolKind(n int) string {
	if k, ok := symbolKinds[n]; ok {
		return k
	}
	return "symbol"
}

// symbolWire covers both documentSymbol shapes: hierarchical
// DocumentSymbol (range/selectionRange/children) and flat
// SymbolInformation (location/containerName).
type symbolWire struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	ContainerName  string `json:"containerName"`
	SelectionRange *Range `json:"selectionRange"`
	Range          *Range `json:"range"`
	Location       *struct {
		Range Range `json:"range"`
	} `json:"location"`
	Children []symbolWire `json:"children"`
}

// DocumentSymbols returns the document outline, flattened depth-first.
func (c *Client) DocumentSymbols(path string) ([]Symbol, error) {
	var raw json.RawMessage
	params := map[string]any{"textDocument": map[string]any{"uri": pathToURI(path)}}
	if err := c.call("textDocument/documentSymbol", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []symbolWire
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("lsp: documentSymbol result: %w", err)
	}
	var out []Symbol
	var walk func(level []symbolWire, depth int)
	walk = func(level []symbolWire, depth int) {
		for _, it := range level {
			var rng Range
			switch {
			case it.SelectionRange != nil:
				rng = *it.SelectionRange
			case it.Range != nil:
				rng = *it.Range
			case it.Location != nil:
				rng = it.Location.Range
			}
			name := it.Name
			if it.ContainerName != "" {
				name = strings.TrimSpace(it.ContainerName) + "." + name
			}
			out = append(out, Symbol{
				Name: name, Kind: symbolKind(it.Kind),
				Line: rng.Start.Line, Col: rng.Start.Character, Depth: depth,
			})
			walk(it.Children, depth+1)
		}
	}
	walk(items, 0)
	return out, nil
}
