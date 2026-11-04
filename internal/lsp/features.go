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

// Location is a definition/reference target.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// PathFor resolves a location's URI to a filesystem path.
func (l Location) PathFor() string { return uriToPath(l.URI) }

// Definition returns the definition location(s) at a position.
func (c *Client) Definition(path string, line, col int) ([]Location, error) {
	return c.locations("textDocument/definition", path, line, col, nil)
}

// References returns references to the symbol at a position.
func (c *Client) References(path string, line, col int) ([]Location, error) {
	return c.locations("textDocument/references", path, line, col,
		map[string]any{"context": map[string]any{"includeDeclaration": true}})
}

// locations handles the Location | []Location result shapes.
func (c *Client) locations(method, path string, line, col int, extra map[string]any) ([]Location, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	for k, v := range extra {
		params[k] = v
	}
	var raw json.RawMessage
	if err := c.call(method, params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one Location
	if err := json.Unmarshal(raw, &one); err == nil && one.URI != "" {
		return []Location{one}, nil
	}
	var many []Location
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("lsp: %s result: %w", method, err)
	}
	return many, nil
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

// PrepareRename asks the server whether the symbol at a position can be
// renamed and, if so, the range it would rewrite. ok is false when the
// server declines (null result or neither a range nor defaultBehavior).
// A non-nil error means the request failed or the method is unsupported;
// callers may fall back to a client-side range in that case.
func (c *Client) PrepareRename(path string, line, col int) (Range, bool, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := c.call("textDocument/prepareRename", params, &raw); err != nil {
		return Range{}, false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return Range{}, false, nil
	}
	// Three wire shapes: {start,end} | {range,placeholder} | {defaultBehavior}.
	var obj struct {
		Start           *Position `json:"start"`
		End             *Position `json:"end"`
		Range           *Range    `json:"range"`
		DefaultBehavior bool      `json:"defaultBehavior"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Range{}, false, fmt.Errorf("lsp: prepareRename result: %w", err)
	}
	switch {
	case obj.Start != nil && obj.End != nil:
		return Range{Start: *obj.Start, End: *obj.End}, true, nil
	case obj.Range != nil:
		return *obj.Range, true, nil
	case obj.DefaultBehavior:
		return Range{Start: Position{Line: line, Character: col}, End: Position{Line: line, Character: col}}, true, nil
	}
	return Range{}, false, nil
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

// ExecuteCommand runs a server-side command from a code action. It is a
// request (the spec's), so the server's error comes back to the user.
func (c *Client) ExecuteCommand(cmd Command) error {
	var raw json.RawMessage
	return c.call("workspace/executeCommand", map[string]any{
		"command":   cmd.Command,
		"arguments": cmd.Arguments,
	}, &raw)
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

// SignatureHelp is the active signature of a call, flattened for the
// popup: Label is the signature text, [ParamStart,ParamEnd) the active
// parameter in rune offsets of Label (equal when none is active).
type SignatureHelp struct {
	Label                string
	ParamStart, ParamEnd int
	Doc                  string
}

// SignatureTriggers reports the characters that should request signature
// help. A server that reports no capabilities at all (test doubles) gets
// the conventional "(" and ","; one that reports some and omits
// signatureHelpProvider is never asked (ADR-0011: no guessing).
func (c *Client) SignatureTriggers() []string {
	if c.caps == nil {
		return []string{"(", ","}
	}
	raw, ok := c.caps["signatureHelpProvider"]
	if !ok {
		return nil
	}
	var p struct {
		Trigger   []string `json:"triggerCharacters"`
		Retrigger []string `json:"retriggerCharacters"`
	}
	_ = json.Unmarshal(raw, &p) // `true` (no options) leaves both empty
	return append(p.Trigger, p.Retrigger...)
}

// SignatureHelp asks for the signature of the call around a position and
// resolves its active signature and parameter. Nil when the server has
// none (null result or no signatures).
func (c *Client) SignatureHelp(path string, line, col int) (*SignatureHelp, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := c.call("textDocument/signatureHelp", params, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var res struct {
		Signatures []struct {
			Label           string          `json:"label"`
			Documentation   json.RawMessage `json:"documentation"`
			ActiveParameter *int            `json:"activeParameter"`
			Parameters      []struct {
				Label json.RawMessage `json:"label"`
			} `json:"parameters"`
		} `json:"signatures"`
		ActiveSignature *int `json:"activeSignature"`
		ActiveParameter *int `json:"activeParameter"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("lsp: signatureHelp result: %w", err)
	}
	if len(res.Signatures) == 0 {
		return nil, nil
	}
	si := 0
	if res.ActiveSignature != nil && *res.ActiveSignature >= 0 && *res.ActiveSignature < len(res.Signatures) {
		si = *res.ActiveSignature
	}
	sig := res.Signatures[si]
	out := &SignatureHelp{Label: sig.Label}
	if len(sig.Documentation) > 0 {
		out.Doc = flattenContents(sig.Documentation)
	}
	pi := -1 // the signature's own activeParameter beats the response's
	switch {
	case sig.ActiveParameter != nil:
		pi = *sig.ActiveParameter
	case res.ActiveParameter != nil:
		pi = *res.ActiveParameter
	}
	if pi < 0 || pi >= len(sig.Parameters) {
		return out, nil
	}
	var name string
	var span []int
	pl := sig.Parameters[pi].Label
	if json.Unmarshal(pl, &name) == nil {
		// A substring of the signature label (first occurrence).
		if i := strings.Index(sig.Label, name); i >= 0 && name != "" {
			out.ParamStart = len([]rune(sig.Label[:i]))
			out.ParamEnd = out.ParamStart + len([]rune(name))
		}
	} else if json.Unmarshal(pl, &span) == nil && len(span) == 2 && span[0] <= span[1] {
		out.ParamStart, out.ParamEnd = RuneCol(sig.Label, span[0]), RuneCol(sig.Label, span[1])
	}
	return out, nil
}
