package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// gitTools is the read-only git surface (F-030 P1). It operates on the
// turn's working directory — the task worktree when the trigger is bound
// to one, else the workspace root — through the same go-git path the
// editor panel and task commits use. Mutating git (commit/push) is a
// later slice; it needs identity + workflow gates.
func (d Deps) gitTools() []tool {
	if d.WS == nil {
		return nil
	}
	open := func() (*gitcore.Repo, error) {
		if strings.TrimSpace(d.Workdir) == "" {
			return nil, fmt.Errorf("no working directory for this turn")
		}
		if !gitcore.IsRepo(d.Workdir) {
			return nil, fmt.Errorf("not a git repository at %s", d.Workdir)
		}
		return gitcore.Open(d.Workdir)
	}
	xy := func(c byte) byte {
		if c == 0 {
			return ' '
		}
		return c
	}
	return []tool{
		{
			info: mcp.ToolInfo{
				Name:        "git_status",
				Description: "Show the working tree status (index + worktree). Args: {}.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
			parse: func(json.RawMessage) (any, error) { return nil, nil },
			exec: func(_ context.Context, _ any) (string, error) {
				repo, err := open()
				if err != nil {
					return "", err
				}
				st, err := repo.Status()
				if err != nil {
					return "", err
				}
				if len(st) == 0 {
					return "(clean)\n", nil
				}
				var b strings.Builder
				for _, f := range st {
					fmt.Fprintf(&b, "%c%c %s\n", xy(f.X), xy(f.Y), f.Path)
				}
				return b.String(), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "git_log",
				Description: "Show recent commits. Args: {\"limit\": 15}.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Limit int `json:"limit"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				if a.Limit <= 0 {
					a.Limit = 15
				}
				return a, nil
			},
			exec: func(_ context.Context, dec any) (string, error) {
				repo, err := open()
				if err != nil {
					return "", err
				}
				entries, err := repo.Log(dec.(struct {
					Limit int `json:"limit"`
				}).Limit)
				if err != nil {
					return "", err
				}
				if len(entries) == 0 {
					return "(no commits)\n", nil
				}
				var b strings.Builder
				for _, e := range entries {
					fmt.Fprintf(&b, "%s %s (%s)\n", e.Short, e.Message, e.Author)
				}
				return b.String(), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "git_branch",
				Description: "Show the current branch and the branch list. Args: {}.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
			parse: func(json.RawMessage) (any, error) { return nil, nil },
			exec: func(_ context.Context, _ any) (string, error) {
				repo, err := open()
				if err != nil {
					return "", err
				}
				cur, err := repo.CurrentBranch()
				if err != nil {
					return "", err
				}
				branches, err := repo.Branches()
				if err != nil {
					return "", err
				}
				var b strings.Builder
				fmt.Fprintf(&b, "current: %s\n", cur)
				fmt.Fprintf(&b, "branches: %s\n", strings.Join(branches, ", "))
				return b.String(), nil
			},
		},
	}
}
