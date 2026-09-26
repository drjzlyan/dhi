package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
