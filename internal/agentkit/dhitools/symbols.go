package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/untrusted"
	"github.com/drjzlyan/dhi/internal/mcp"
)

type symbolsPlan struct {
	mode, query, path string
}

// symbolsTools is code_symbols (F-145): a repo map from the language
// servers the editor already runs. It rides the ADR-0023 editor seam as
// two LSP ops; the app answers "no language server" plainly.
func (d Deps) symbolsTools() []tool {
	if d.WS == nil {
		return nil
	}
	return []tool{{
		info: mcp.ToolInfo{
			Name: "code_symbols",
			Description: "Find code symbols through the running language servers (no embeddings). " +
				"Args: {\"mode\":\"workspace\",\"query\":\"Name\",\"path\":\"<member>/<rel>\" (optional, picks the language)} lists matching symbols as kind name member/file:line; " +
				"{\"mode\":\"document\",\"path\":\"<member>/<rel>\"} outlines one file. Capped at 200. " +
				"Says so when no language server is running for the language; installs nothing.",
			InputSchema: json.RawMessage(`{"type":"object","required":["mode"],"properties":{"mode":{"type":"string","enum":["workspace","document"]},"query":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a symbolsPlan
			var w struct {
				Mode  string `json:"mode"`
				Query string `json:"query"`
				Path  string `json:"path"`
			}
			if err := args(raw, &w); err != nil {
				return nil, err
			}
			a = symbolsPlan{mode: strings.TrimSpace(w.Mode), query: strings.TrimSpace(w.Query), path: strings.TrimSpace(w.Path)}
			switch a.mode {
			case "workspace":
				if a.query == "" {
					return nil, fmt.Errorf("query is required for workspace mode")
				}
			case "document":
				if a.path == "" {
					return nil, fmt.Errorf("path is required for document mode")
				}
			default:
				return nil, fmt.Errorf("mode must be workspace or document")
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			if d.Editor == nil {
				return "", fmt.Errorf("editor unavailable (no editor surface this session)")
			}
			p := dec.(symbolsPlan)
			if p.path != "" {
				// Same jail and member scope as every other path tool.
				if _, err := d.resolveVPath(p.path); err != nil {
					return "", err
				}
			}
			text, err := d.Editor.LSP(ctx, "symbols_"+p.mode, p.path, 0, 0, p.query)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(text) == "" {
				return "(no result)", nil
			}
			// Names come from repo files anyone may have written (F-122).
			// Plain status answers carry no repo text and stay bare.
			if strings.HasPrefix(text, "no language server") || strings.HasPrefix(text, "(no ") {
				return text, nil
			}
			return untrusted.Wrap("symbols", text), nil
		},
	}}
}
