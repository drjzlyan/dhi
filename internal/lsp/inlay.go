package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Inlay hints (LSP 3.17, F-169): short labels a server wants shown inline,
// such as the inferred type after a variable or a parameter name before
// an argument.

// InlayHint is one hint. Line/Col are the LSP position (Col in UTF-16
// units); the label is flattened to text.
type InlayHint struct {
	Line, Col         int
	Label             string
	Kind              int // 1 type, 2 parameter, 0 other
	PadLeft, PadRight bool
}

// Inlay hint kinds (LSP InlayHintKind).
const (
	InlayType      = 1
	InlayParameter = 2
)

// CanInlayHints reports whether the server advertises inlayHintProvider.
// A server that reports nothing is not asked (ADR-0011: no guessing).
func (c *Client) CanInlayHints() bool {
	v, ok := c.caps["inlayHintProvider"]
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(v))
	return s != "false" && s != "null" && s != ""
}

// InlayHints asks for the hints inside rng.
func (c *Client) InlayHints(path string, rng Range) ([]InlayHint, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"range": map[string]any{
			"start": map[string]any{"line": rng.Start.Line, "character": rng.Start.Character},
			"end":   map[string]any{"line": rng.End.Line, "character": rng.End.Character},
		},
	}
	if err := c.call("textDocument/inlayHint", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []struct {
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
		Label        json.RawMessage `json:"label"`
		Kind         int             `json:"kind"`
		PaddingLeft  bool            `json:"paddingLeft"`
		PaddingRight bool            `json:"paddingRight"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("lsp: inlayHint result: %w", err)
	}
	out := make([]InlayHint, 0, len(items))
	for _, it := range items {
		label := inlayLabel(it.Label)
		if label == "" {
			continue
		}
		out = append(out, InlayHint{Line: it.Position.Line, Col: it.Position.Character,
			Label: label, Kind: it.Kind, PadLeft: it.PaddingLeft, PadRight: it.PaddingRight})
	}
	return out, nil
}

// inlayLabel flattens a label: a string, or label parts whose values join.
func inlayLabel(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Value)
	}
	return b.String()
}

// goplsHints are the standard gopls hints DHI turns on with the
// editor.inlay_hints setting: inferred types and parameter names.
var goplsHints = []string{"assignVariableTypes", "compositeLiteralFields",
	"functionTypeParameters", "parameterNames", "rangeVariableTypes"}

// SetInlayHints tells a server that needs it to emit (or stop emitting)
// hints. Only gopls is known to need this: its hints are off until its
// "hints" settings say so. The options are sent with
// workspace/didChangeConfiguration and answered to workspace/configuration;
// other servers emit hints from their own defaults and are left alone.
func (c *Client) SetInlayHints(on bool) error {
	if !strings.EqualFold(c.serverName, "gopls") {
		return nil
	}
	hints := map[string]any{}
	for _, h := range goplsHints {
		hints[h] = on
	}
	opts := map[string]any{"hints": hints}
	c.optMu.Lock()
	c.options = map[string]any{"gopls": opts}
	c.optMu.Unlock()
	return c.notify("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{"gopls": opts}})
}

// configuration answers workspace/configuration: one entry per requested
// item, the stored options for a known section and an empty object
// otherwise (servers reject null).
func (c *Client) configuration(params json.RawMessage) []any {
	var p struct {
		Items []struct {
			Section string `json:"section"`
		} `json:"items"`
	}
	_ = json.Unmarshal(params, &p)
	c.optMu.Lock()
	defer c.optMu.Unlock()
	out := make([]any, len(p.Items))
	for i, it := range p.Items {
		out[i] = map[string]any{}
		if v, ok := c.options[it.Section]; ok {
			out[i] = v
		}
	}
	return out
}
