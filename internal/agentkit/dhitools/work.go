package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/mcp"
)

var workSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// workTools serves work_branch (F-086): a chat turn that changes code
// gets the same card, branch and worktree a task turn gets, instead of
// editing the shared checkout on its default branch.
func (d Deps) workTools() []tool {
	if d.WorkBranch == nil {
		return nil
	}
	type workArgs struct {
		Slug   string `json:"slug"`
		Title  string `json:"title"`
		Member string `json:"member"`
		Carry  *bool  `json:"carry"`
	}
	return []tool{{
		info: mcp.ToolInfo{
			Name: "work_branch",
			Description: "Move this conversation's code work onto its own task card, branch and worktree before you edit or commit. " +
				"Args: {\"slug\":\"short-kebab-id\",\"title\":\"what and why\", optional \"member\":\"repo\" (required with several), " +
				"optional \"carry\": true (default) moves uncommitted changes already made in the shared checkout into the worktree}. " +
				"An existing slug you own is resumed. After it succeeds, end your run with one short line: DHI continues you in the worktree.",
			InputSchema: json.RawMessage(`{"type":"object","required":["slug","title"],"properties":{"slug":{"type":"string"},"title":{"type":"string"},"member":{"type":"string"},"carry":{"type":"boolean"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a workArgs
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			a.Slug, a.Title = strings.TrimSpace(a.Slug), strings.TrimSpace(a.Title)
			if !workSlugRe.MatchString(a.Slug) {
				return nil, fmt.Errorf("slug must be short lowercase kebab-case (a-z, 0-9, -)")
			}
			if a.Title == "" {
				return nil, fmt.Errorf("title is required")
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(workArgs)
			carry := a.Carry == nil || *a.Carry
			return d.WorkBranch(ctx, a.Slug, a.Title, strings.TrimSpace(a.Member), carry)
		},
	}}
}
