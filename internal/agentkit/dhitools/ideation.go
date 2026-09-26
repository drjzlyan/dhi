package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/mcp"
)

// ideationTools is the read-only ideation surface (F-030 P1): list
// sessions and read an artifact's content. Writing artifacts uses the
// existing file tools under the reserved `.dhi/sessions/` VPath.
func (d Deps) ideationTools() []tool {
	if d.Sessions == nil {
		return nil
	}
	return []tool{
		{
			info: mcp.ToolInfo{
				Name:        "ideation_list",
				Description: "List ideation sessions. Args: {}. Returns id, name, topic and artifact count.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
			parse: func(json.RawMessage) (any, error) { return nil, nil },
			exec: func(_ context.Context, _ any) (string, error) {
				sessions := d.Sessions.Sessions()
				if len(sessions) == 0 {
					return "(no sessions)\n", nil
				}
				var b strings.Builder
				for _, s := range sessions {
					fmt.Fprintf(&b, "%s  %s — %s (%d artifact(s))\n",
						s.ID, s.Name, s.Topic, len(s.Artifacts))
				}
				return b.String(), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "ideation_read",
				Description: "Read an ideation artifact. Args: {\"session\": \"<id>\", \"path\": \"plan.md\"}.",
				InputSchema: json.RawMessage(`{"type":"object","required":["session","path"],"properties":{"session":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Session string `json:"session"`
					Path    string `json:"path"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.Session) == "" || strings.TrimSpace(a.Path) == "" {
					return nil, fmt.Errorf("session and path are required")
				}
				rel := strings.TrimSpace(a.Path)
				if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
					return nil, fmt.Errorf("path %q must be relative to the session folder", rel)
				}
				if _, ok := d.Sessions.Get(a.Session); !ok {
					return nil, fmt.Errorf("unknown session %q", a.Session)
				}
				abs := d.Sessions.ArtifactPath(a.Session, rel)
				if _, err := os.Stat(abs); err != nil {
					return nil, fmt.Errorf("unknown artifact %q in %s", rel, a.Session)
				}
				return artifactPlan{session: a.Session, rel: rel, abs: abs}, nil
			},
			exec: func(_ context.Context, dec any) (string, error) {
				p := dec.(artifactPlan)
				info, err := os.Stat(p.abs)
				if err != nil {
					return "", fmt.Errorf("%s: %w", p.rel, err)
				}
				if info.Size() > maxReadBytes {
					return "", fmt.Errorf("%s is %d bytes (over the %d-byte read cap)", p.rel, info.Size(), maxReadBytes)
				}
				data, err := os.ReadFile(p.abs)
				if err != nil {
					return "", fmt.Errorf("%s: %w", p.rel, err)
				}
				return string(data), nil
			},
		},
	}
}

type artifactPlan struct {
	session, rel, abs string
}
