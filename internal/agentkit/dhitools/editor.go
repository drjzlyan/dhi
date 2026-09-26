package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/mcp"
)

// EditorAPI is the app-owned seam the runtime calls for editor tools
// (ADR-0023): the runtime asks, the app routes to the editor surface on
// its own loop. A nil seam makes every editor tool refuse by name.
// Paths are VPaths (member/rel), never absolute.
type EditorAPI interface {
	Open(ctx context.Context, paths []string) error
	Reveal(ctx context.Context, path string) error
	// Apply replaces exact text in a file: through the live buffer when
	// the file is open (one undo step), else the file on disk. The seam
	// refuses absent/ambiguous matches unless all.
	Apply(ctx context.Context, path, old, new string, all bool) error
}

// editorTools is the editor navigation surface (F-030 P1, ADR-0023).
// apply-edit and the LSP verbs land on the same seam next.
func (d Deps) editorTools() []tool {
	if d.WS == nil {
		return nil
	}
	return []tool{
		{
			info: mcp.ToolInfo{
				Name:        "editor_open",
				Description: "Open files in the editor (reveal + focus). Args: {\"paths\":[\"<member>/<rel>\", ...]}.",
				InputSchema: json.RawMessage(`{"type":"object","required":["paths"],"properties":{"paths":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Paths []string `json:"paths"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				var clean []string
				for _, p := range a.Paths {
					if p = strings.TrimSpace(p); p != "" {
						clean = append(clean, p)
					}
				}
				if len(clean) == 0 {
					return nil, fmt.Errorf("at least one path is required")
				}
				return clean, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				if d.Editor == nil {
					return "", fmt.Errorf("editor unavailable (no editor surface this session)")
				}
				paths := dec.([]string)
				if err := d.Editor.Open(ctx, paths); err != nil {
					return "", err
				}
				return fmt.Sprintf("opened %d path(s)", len(paths)), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "editor_reveal",
				Description: "Reveal a file in the editor without changing focus intent. Args: {\"path\": \"<member>/<rel>\"}.",
				InputSchema: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Path string `json:"path"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				p := strings.TrimSpace(a.Path)
				if p == "" {
					return nil, fmt.Errorf("path is required")
				}
				return p, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				if d.Editor == nil {
					return "", fmt.Errorf("editor unavailable (no editor surface this session)")
				}
				if err := d.Editor.Reveal(ctx, dec.(string)); err != nil {
					return "", err
				}
				return "revealed " + dec.(string), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "editor_apply_edit",
				Description: "Replace exact text in a file through the editor (live buffer when open, one undo step). Args: {\"path\": \"<member>/<rel>\", \"old\": \"...\", \"new\": \"...\", \"replace_all\": false}. Refuses absent/ambiguous matches. Mutating: crosses approvals.",
				InputSchema: json.RawMessage(`{"type":"object","required":["path","old","new"],"properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}},"additionalProperties":false}`),
			},
			mutate: true,
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Path       string `json:"path"`
					Old        string `json:"old"`
					New        string `json:"new"`
					ReplaceAll bool   `json:"replace_all"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.Path) == "" {
					return nil, fmt.Errorf("path is required")
				}
				if a.Old == "" {
					return nil, fmt.Errorf("old text is required")
				}
				return applyPlan{path: a.Path, old: a.Old, new: a.New, all: a.ReplaceAll}, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				if d.Editor == nil {
					return "", fmt.Errorf("editor unavailable (no editor surface this session)")
				}
				p := dec.(applyPlan)
				if err := d.Editor.Apply(ctx, p.path, p.old, p.new, p.all); err != nil {
					return "", err
				}
				return "applied edit to " + p.path, nil
			},
		},
	}
}

type applyPlan struct {
	path, old, new string
	all            bool
}
