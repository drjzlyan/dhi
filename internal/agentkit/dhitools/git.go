package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// GitCLI is the narrow git-CLI seam the diff tool needs (go-git has no
// patch producer). Satisfied by *gitcore.Runner — the hermetic git
// binary with its declared env.
type GitCLI interface {
	Run(ctx context.Context, dir string, args ...string) (string, string, error)
}

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
		{
			info: mcp.ToolInfo{
				Name:        "git_diff",
				Description: "Show the working-tree diff. Args: {\"staged\": false}. Empty output means no changes.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"staged":{"type":"boolean"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Staged bool `json:"staged"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				return a, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				if d.Git == nil {
					return "", fmt.Errorf("git CLI unavailable (run bootstrap)")
				}
				if strings.TrimSpace(d.Workdir) == "" {
					return "", fmt.Errorf("no working directory for this turn")
				}
				argv := []string{"diff", "--no-color"}
				if dec.(struct {
					Staged bool `json:"staged"`
				}).Staged {
					argv = append(argv, "--staged")
				}
				out, _, err := d.Git.Run(ctx, d.Workdir, argv...)
				if err != nil {
					return "", err
				}
				if strings.TrimSpace(out) == "" {
					return "(no changes)\n", nil
				}
				return out, nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "git_commit",
				Description: "Stage all changes and commit them as the user's git identity. Args: {\"message\": \"...\"}. Mutating: crosses approvals. Refuses with nothing staged.",
				InputSchema: json.RawMessage(`{"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Message string `json:"message"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.Message) == "" {
					return nil, fmt.Errorf("commit message is required")
				}
				if d.Conventions != nil {
					if err := d.Conventions.Commit.CheckCommit(a.Message); err != nil {
						return nil, fmt.Errorf("conventions: %w", err)
					}
				}
				// Workflow gate (F-031): worktree-before-commit is a hard
				// block; refuse before spending an approval.
				if reasons := d.checkGate("git:commit"); len(reasons) > 0 {
					return nil, fmt.Errorf("workflow blocks commit: %s", strings.Join(reasons, "; "))
				}
				// Validate the target repo before spending an approval.
				if _, err := open(); err != nil {
					return nil, err
				}
				return a, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				repo, err := open()
				if err != nil {
					return "", err
				}
				if d.Identity == nil {
					return "", fmt.Errorf("%w", gitcore.ErrIdentityUnset)
				}
				id, err := d.Identity(ctx)
				if err != nil {
					return "", err
				}
				if err := repo.Stage("."); err != nil {
					return "", err
				}
				msg := dec.(struct {
					Message string `json:"message"`
				}).Message
				if d.Conventions != nil {
					msg = d.Conventions.Commit.Finalize(msg)
				}
				hash, err := repo.Commit(gitcore.CommitOptions{
					Message: msg,
					Author:  id.Name, Email: id.Email,
				})
				if err != nil {
					return "", err
				}
				return "committed " + hash[:min(7, len(hash))], nil
			},
		},
	}
}
