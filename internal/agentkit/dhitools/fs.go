package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// maxReadBytes bounds one read so an agent cannot pull a huge file into
// the model context; the refusal names the cap and the fix (use search).
const maxReadBytes = 1 << 20 // 1 MiB

// maxGlobMatches bounds a glob result so a broad pattern cannot flood a
// turn; the refusal names the cap.
const maxGlobMatches = 200

type readArgs struct {
	Path string `json:"path"`
}

type globArgs struct {
	Pattern string `json:"pattern"`
}

// writePlan/patchPlan carry the parsed, VPath-resolved target so a bad
// path refuses BEFORE the approval gate (no human prompt for a schema
// error), and exec never re-resolves (no TOCTOU on the jail decision).
type writePlan struct {
	path, abs, content string
}

type patchPlan struct {
	path, abs, old, new string
	all                 bool
}

// fsTools is the filesystem read surface (F-030 P1): paths are VPaths
// resolved through the workspace jail, so an agent addresses files the
// same way the IDE does and can never escape a member.
func (d Deps) fsTools() []tool {
	if d.WS == nil {
		return nil
	}
	var out []tool
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "read",
			Description: "Read a workspace file. Args: {\"path\": \"<member>/<rel-path>\"}. Refuses paths outside members and files over 1 MiB.",
			InputSchema: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a readArgs
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Path) == "" {
				return nil, fmt.Errorf("path is required")
			}
			return a, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			abs, err := d.resolveVPath(dec.(readArgs).Path)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(abs)
			if err != nil {
				return "", fmt.Errorf("%s: %w", dec.(readArgs).Path, err)
			}
			if info.IsDir() {
				return "", fmt.Errorf("%s is a directory (use list)", dec.(readArgs).Path)
			}
			if info.Size() > maxReadBytes {
				return "", fmt.Errorf("%s is %d bytes (over the %d-byte read cap; use workspace_search)",
					dec.(readArgs).Path, info.Size(), maxReadBytes)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return "", fmt.Errorf("%s: %w", dec.(readArgs).Path, err)
			}
			return string(data), nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "list",
			Description: "List a workspace directory. Args: {\"path\": \"<member>[/<rel-dir>]\"}. Directories carry a trailing slash.",
			InputSchema: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a readArgs
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Path) == "" {
				return nil, fmt.Errorf("path is required")
			}
			return a, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			abs, err := d.resolveVPath(dec.(readArgs).Path)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(abs)
			if err != nil {
				return "", fmt.Errorf("%s: %w", dec.(readArgs).Path, err)
			}
			var b strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				b.WriteString(name)
				b.WriteString("\n")
			}
			if b.Len() == 0 {
				return "(empty)\n", nil
			}
			return b.String(), nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "glob",
			Description: "Match files by glob pattern relative to each member root. Args: {\"pattern\": \"pkg/*.go\"}. Returns VPaths.",
			InputSchema: json.RawMessage(`{"type":"object","required":["pattern"],"properties":{"pattern":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a globArgs
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			p := strings.TrimSpace(a.Pattern)
			if p == "" {
				return nil, fmt.Errorf("pattern is required")
			}
			if strings.Contains(p, "..") || filepath.IsAbs(p) {
				return nil, fmt.Errorf("pattern %q must be a member-relative glob (no .. or absolute paths)", p)
			}
			return globArgs{Pattern: filepath.ToSlash(p)}, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			pattern := dec.(globArgs).Pattern
			var matches []string
			for _, mem := range d.WS.Members() {
				found, err := filepath.Glob(filepath.Join(mem.Path, filepath.FromSlash(pattern)))
				if err != nil {
					return "", fmt.Errorf("pattern %q: %w", pattern, err)
				}
				for _, abs := range found {
					v, err := d.WS.VPathFor(abs)
					if err != nil {
						continue // outside a member (should not happen)
					}
					matches = append(matches, v.String())
				}
			}
			sort.Strings(matches)
			if len(matches) == 0 {
				return "(no matches)\n", nil
			}
			var b strings.Builder
			for i, m := range matches {
				if i >= maxGlobMatches {
					fmt.Fprintf(&b, "(more matches omitted)\n")
					break
				}
				b.WriteString(m)
				b.WriteString("\n")
			}
			return b.String(), nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "write",
			Description: "Write a workspace file (create or overwrite; parent dirs are created). Args: {\"path\": \"<member>/<rel-path>\", \"content\": \"...\"}. Mutating: crosses approvals.",
			InputSchema: json.RawMessage(`{"type":"object","required":["path","content"],"properties":{"path":{"type":"string"},"content":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Path) == "" {
				return nil, fmt.Errorf("path is required")
			}
			abs, err := d.resolveVPath(a.Path)
			if err != nil {
				return nil, err
			}
			return writePlan{path: a.Path, abs: abs, content: a.Content}, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			p := dec.(writePlan)
			if info, err := os.Stat(p.abs); err == nil && info.IsDir() {
				return "", fmt.Errorf("%s is a directory", p.path)
			}
			if err := os.MkdirAll(filepath.Dir(p.abs), 0o755); err != nil {
				return "", fmt.Errorf("%s: %w", p.path, err)
			}
			content := p.content
			// A NEW file gets the team's copyright header (F-042); an
			// existing file is overwritten exactly as given.
			if _, err := os.Stat(p.abs); os.IsNotExist(err) && d.Conventions != nil {
				content = d.Conventions.Copyright.EnsureHeader(p.abs, content, time.Now())
			}
			if err := os.WriteFile(p.abs, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("%s: %w", p.path, err)
			}
			return fmt.Sprintf("wrote %s (%d bytes)", p.path, len(content)), nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "patch",
			Description: "Replace exact text in a workspace file. Args: {\"path\": \"<member>/<rel-path>\", \"old\": \"...\", \"new\": \"...\", \"replace_all\": false}. Refuses when `old` is absent or ambiguous unless replace_all. Mutating: crosses approvals.",
			InputSchema: json.RawMessage(`{"type":"object","required":["path","old","new"],"properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}},"additionalProperties":false}`),
		},
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
				return nil, fmt.Errorf("old text is required (empty `old` would match everywhere)")
			}
			abs, err := d.resolveVPath(a.Path)
			if err != nil {
				return nil, err
			}
			return patchPlan{path: a.Path, abs: abs, old: a.Old, new: a.New, all: a.ReplaceAll}, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			p := dec.(patchPlan)
			data, err := os.ReadFile(p.abs)
			if err != nil {
				return "", fmt.Errorf("%s: %w", p.path, err)
			}
			content := string(data)
			count := strings.Count(content, p.old)
			switch {
			case count == 0:
				return "", fmt.Errorf("old text not found in %s", p.path)
			case count > 1 && !p.all:
				return "", fmt.Errorf("old text appears %d times in %s (set replace_all or narrow it)", count, p.path)
			}
			var replaced string
			if p.all {
				replaced = strings.ReplaceAll(content, p.old, p.new)
			} else {
				replaced = strings.Replace(content, p.old, p.new, 1)
			}
			if err := os.WriteFile(p.abs, []byte(replaced), 0o644); err != nil {
				return "", fmt.Errorf("%s: %w", p.path, err)
			}
			return fmt.Sprintf("patched %s (%d replacement(s))", p.path, count), nil
		},
	})
	return out
}

// resolveVPath parses a textual vpath and maps it into the member jail,
// refusing escapes and unknown members by name.
func (d Deps) resolveVPath(p string) (string, error) {
	v, err := workspace.ParseVPath(p)
	if err != nil {
		return "", err
	}
	return d.WS.Resolve(v)
}
