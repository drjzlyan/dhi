package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Position is a 0-based line/character offset (LSP wire shape).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range spans two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// TextEdit replaces Range with NewText.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// Command is a server-side executable action.
type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

// CodeAction is one quickfix/refactor offering.
type CodeAction struct {
	Title   string         `json:"title"`
	Kind    string         `json:"kind,omitempty"`
	Edit    *WorkspaceEdit `json:"edit,omitempty"`
	Command *Command       `json:"command,omitempty"`
}

// WorkspaceEdit is a set of per-file edits. The wire allows either a
// `changes` map or a `documentChanges` array; both flatten into Files.
type WorkspaceEdit struct {
	Files []FileEdit
}

// FileEdit is the edits for one document (URI-flattened).
type FileEdit struct {
	URI   string
	Edits []TextEdit
}

// UnmarshalJSON accepts both WorkspaceEdit wire shapes.
func (w *WorkspaceEdit) UnmarshalJSON(b []byte) error {
	var raw struct {
		Changes         map[string][]TextEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []TextEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for uri, edits := range raw.Changes {
		w.Files = append(w.Files, FileEdit{URI: uri, Edits: edits})
	}
	for _, d := range raw.DocumentChanges {
		w.Files = append(w.Files, FileEdit{URI: d.TextDocument.URI, Edits: d.Edits})
	}
	return nil
}

// PathFor resolves a FileEdit's URI to a filesystem path.
func (f FileEdit) PathFor() string { return uriToPath(f.URI) }

// Hover is flattened hover content (plain text).
type Hover struct {
	Contents string
}

// flattenContents folds the hover `contents` wire shapes (string |
// {value,kind} | array of either) into plain text, trimming code
// fences so the popup renders readably.
func flattenContents(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	var parts []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			parts = append(parts, t)
		case map[string]any:
			if s, ok := t["value"].(string); ok {
				parts = append(parts, s)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return strings.TrimSpace(trimFences(strings.Join(parts, "\n")))
}

// trimFences removes ```lang … ``` marker lines but keeps their
// content (gopls puts the signature inside the fence).
func trimFences(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// Hover requests documentation at a position.
func (c *Client) Hover(path string, line, col int) (*Hover, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := c.call("textDocument/hover", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var result struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("lsp: hover result: %w", err)
	}
	return &Hover{Contents: flattenContents(result.Contents)}, nil
}

// Rename asks the server for edits renaming the symbol at a position.
func (c *Client) Rename(path string, line, col int, newName string) (*WorkspaceEdit, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
		"newName":      newName,
	}
	if err := c.call("textDocument/rename", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var edit WorkspaceEdit
	if err := json.Unmarshal(raw, &edit); err != nil {
		return nil, fmt.Errorf("lsp: rename result: %w", err)
	}
	return &edit, nil
}

// CodeAction requests quickfixes for a range, seeded with the
// diagnostics the UI already knows about on that line.
func (c *Client) CodeAction(path string, rng Range, diags []Diagnostic) ([]CodeAction, error) {
	wireDiags := make([]map[string]any, 0, len(diags))
	for _, d := range diags {
		wireDiags = append(wireDiags, map[string]any{
			"range":    Range{Start: Position{Line: d.Line, Character: d.Col}},
			"severity": d.Severity,
			"message":  d.Message,
		})
	}
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"range":        rng,
		"context":      map[string]any{"diagnostics": wireDiags},
	}
	if err := c.call("textDocument/codeAction", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var actions []CodeAction
	if err := json.Unmarshal(raw, &actions); err != nil {
		return nil, fmt.Errorf("lsp: codeAction result: %w", err)
	}
	return actions, nil
}

// ExecuteCommand runs a server-side command from a code action.
func (c *Client) ExecuteCommand(cmd Command) error {
	return c.notify("workspace/executeCommand", map[string]any{
		"command":   cmd.Command,
		"arguments": cmd.Arguments,
	})
}

// decodeApplyEdit parses workspace/applyEdit params.
func decodeApplyEdit(params json.RawMessage) (*WorkspaceEdit, bool) {
	var p struct {
		Edit WorkspaceEdit `json:"edit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, false
	}
	return &p.Edit, true
}
