package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CallItem is one node of the call hierarchy (F-173): a function or
// method. Raw is the server's own item, sent back verbatim to resolve
// its calls (the item carries an opaque data token).
type CallItem struct {
	Name   string
	Kind   string
	Detail string
	URI    string
	Range  Range // the whole declaration
	Select Range // the name; where a jump lands
	// Sites are the call expressions inside the other function (the
	// caller's file for incoming calls, the queried function for outgoing).
	Sites []Range
	Raw   json.RawMessage
}

// PathFor resolves the item's URI to a filesystem path.
func (i CallItem) PathFor() string { return uriToPath(i.URI) }

// supports reports whether the server advertises a capability. A server
// that reports no capabilities at all gets the benefit of the doubt (test
// doubles); one that reports capabilities but omits it is not asked.
func (c *Client) supports(key string) bool {
	if c.caps == nil {
		return true
	}
	v, ok := c.caps[key]
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(v))
	return s != "false" && s != "null" && s != ""
}

// CanCallHierarchy reports whether the server offers call hierarchy.
func (c *Client) CanCallHierarchy() bool { return c.supports("callHierarchyProvider") }

type wireCallItem struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	Detail         string `json:"detail"`
	URI            string `json:"uri"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
}

func decodeCallItem(raw json.RawMessage) (CallItem, error) {
	var w wireCallItem
	if err := json.Unmarshal(raw, &w); err != nil {
		return CallItem{}, err
	}
	return CallItem{Name: w.Name, Kind: symbolKind(w.Kind), Detail: w.Detail, URI: w.URI,
		Range: w.Range, Select: w.SelectionRange, Raw: raw}, nil
}

// PrepareCallHierarchy resolves the function at a position (usually one
// item; none when the cursor is not on a callable).
func (c *Client) PrepareCallHierarchy(path string, line, col int) ([]CallItem, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	var raw []json.RawMessage
	if err := c.call("textDocument/prepareCallHierarchy", params, &raw); err != nil {
		return nil, err
	}
	out := make([]CallItem, 0, len(raw))
	for _, r := range raw {
		it, err := decodeCallItem(r)
		if err != nil {
			return nil, fmt.Errorf("lsp: prepareCallHierarchy result: %w", err)
		}
		out = append(out, it)
	}
	return out, nil
}

// IncomingCalls lists the callers of item.
func (c *Client) IncomingCalls(item CallItem) ([]CallItem, error) {
	return c.calls("callHierarchy/incomingCalls", "from", item)
}

// OutgoingCalls lists what item calls.
func (c *Client) OutgoingCalls(item CallItem) ([]CallItem, error) {
	return c.calls("callHierarchy/outgoingCalls", "to", item)
}

func (c *Client) calls(method, side string, item CallItem) ([]CallItem, error) {
	var res []map[string]json.RawMessage
	if err := c.call(method, map[string]any{"item": item.Raw}, &res); err != nil {
		return nil, err
	}
	out := make([]CallItem, 0, len(res))
	for _, r := range res {
		it, err := decodeCallItem(r[side])
		if err != nil {
			return nil, fmt.Errorf("lsp: %s result: %w", method, err)
		}
		_ = json.Unmarshal(r["fromRanges"], &it.Sites)
		out = append(out, it)
	}
	return out, nil
}
