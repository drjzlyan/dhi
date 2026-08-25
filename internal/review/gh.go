package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// PRMeta is the slice of GitHub PR metadata the reviewer surface shows
// and the completion flow needs.
type PRMeta struct {
	Number  int
	Title   string
	BaseRef string
	HeadRef string
	HeadSHA string
	Author  string
	URL     string
	Draft   bool
}

// GH is the narrow seam over the host `gh` CLI. It is optional: reviews
// of branches and worktrees never touch it, and PR flows degrade with a
// visible message when unavailable. Tests fake it.
type GH interface {
	Available() bool
	PR(ctx context.Context, repo, number string) (PRMeta, error)
	Diff(ctx context.Context, repo, number string) (string, error)
	PostComment(ctx context.Context, repo, number, body string) error
}

// GHCLI runs the host gh binary. Zero value probes PATH lazily.
type GHCLI struct{}

// NewGHCLI returns the exec-backed gh seam.
func NewGHCLI() *GHCLI { return &GHCLI{} }

// Available reports whether gh exists on PATH.
func (g *GHCLI) Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func (g *GHCLI) run(ctx context.Context, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "gh", args...)
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("review: gh %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// PR fetches metadata for one pull request. repo may be OWNER/REPO or a
// full remote URL (gh accepts both).
func (g *GHCLI) PR(ctx context.Context, repo, number string) (PRMeta, error) {
	out, err := g.run(ctx, "pr", "view", number,
		"--repo", repo,
		"--json", "number,title,baseRefName,headRefName,headRefOid,author,url,isDraft")
	if err != nil {
		return PRMeta{}, err
	}
	var raw struct {
		Number      int             `json:"number"`
		Title       string          `json:"title"`
		BaseRefName string          `json:"baseRefName"`
		HeadRefName string          `json:"headRefName"`
		HeadRefOid  string          `json:"headRefOid"`
		Author      json.RawMessage `json:"author"`
		URL         string          `json:"url"`
		IsDraft     bool            `json:"isDraft"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return PRMeta{}, fmt.Errorf("review: gh pr view: decode: %w", err)
	}
	return PRMeta{
		Number:  raw.Number,
		Title:   raw.Title,
		BaseRef: raw.BaseRefName,
		HeadRef: raw.HeadRefName,
		HeadSHA: raw.HeadRefOid,
		Author:  extractLogin(raw.Author),
		URL:     raw.URL,
		Draft:   raw.IsDraft,
	}, nil
}

// extractLogin reads the author field, which is {"login":"x"} in gh's
// JSON output.
func extractLogin(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var ref struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(raw, &ref); err == nil {
		return ref.Login
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// Diff returns the unified patch text of the PR.
func (g *GHCLI) Diff(ctx context.Context, repo, number string) (string, error) {
	return g.run(ctx, "pr", "diff", number, "--repo", repo)
}

// PostComment posts body as an issue comment on the PR.
func (g *GHCLI) PostComment(ctx context.Context, repo, number, body string) error {
	_, err := g.run(ctx, "pr", "comment", number, "--repo", repo, "--body", body)
	return err
}
