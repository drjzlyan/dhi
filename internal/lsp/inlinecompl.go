package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Inline completion (LSP 3.18, F-181): the server proposes text to insert
// at the cursor, which the editor shows as ghost text.

// InlineItem is one proposal. Text is the text to insert at the cursor
// (or over Range when set); Snippet means Text is in snippet syntax.
type InlineItem struct {
	Text    string
	Snippet bool
	Range   *Range
}

// CanInlineCompletion reports whether the server advertises
// inlineCompletionProvider. A server that reports nothing is not asked
// (ADR-0011: no guessing).
func (c *Client) CanInlineCompletion() bool {
	v, ok := c.caps["inlineCompletionProvider"]
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(v))
	return s != "false" && s != "null" && s != ""
}

// InlineCompletion asks for proposals at a 0-based line and UTF-16 column.
// The trigger is Automatic (typing paused), as the spec means it.
func (c *Client) InlineCompletion(path string, line, col int) ([]InlineItem, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
		"context":      map[string]any{"triggerKind": 2},
	}
	if err := c.call("textDocument/inlineCompletion", params, &raw); err != nil {
		return nil, err
	}
	return decodeInline(raw)
}

// decodeInline reads InlineCompletionItem[] | InlineCompletionList | null.
func decodeInline(raw json.RawMessage) ([]InlineItem, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	type wire struct {
		InsertText json.RawMessage `json:"insertText"`
		Range      *Range          `json:"range"`
	}
	var list []wire
	if err := json.Unmarshal(raw, &list); err != nil {
		var l struct {
			Items []wire `json:"items"`
		}
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("lsp: inlineCompletion result: %w", err)
		}
		list = l.Items
	}
	out := make([]InlineItem, 0, len(list))
	for _, w := range list {
		it := InlineItem{Range: w.Range}
		var s string
		if json.Unmarshal(w.InsertText, &s) == nil {
			it.Text = s
		} else {
			var sv struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			}
			if json.Unmarshal(w.InsertText, &sv) != nil {
				continue
			}
			it.Text, it.Snippet = sv.Value, sv.Kind == "snippet"
		}
		if it.Text == "" {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}
