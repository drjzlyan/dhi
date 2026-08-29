package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
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
	AuthToken(ctx context.Context) (string, error)
	PR(ctx context.Context, repo, number string) (PRMeta, error)
	Diff(ctx context.Context, repo, number string) (string, error)
	PostComment(ctx context.Context, repo, number, body string) error
	PostReviewComment(ctx context.Context, repo, number, commitSHA, path string, line int, side string, body string, inReplyTo int64) error
	CreatePR(ctx context.Context, repo, title, body, base, head string) (PRMeta, error)
	ReviewComments(ctx context.Context, repo, number string) ([]RemoteComment, error)
	IssueComments(ctx context.Context, repo, number string) ([]RemoteComment, error)
}

// RemoteComment is one comment fetched from GitHub: a PR review comment
// (line-anchored, possibly threaded via InReplyTo) or an issue comment
// (Path empty).
type RemoteComment struct {
	ID        int64
	RootID    int64  // resolved thread root (itself when top-level)
	Path      string // "" for issue comments
	Line      int    // new-side line; 0 when outdated
	OrigLine  int    // old-side line
	Side      string // "RIGHT" | "LEFT" | ""
	Body      string
	Author    string
	CreatedAt time.Time
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

// PostReviewComment posts a review comment on a PR at a specific file/line.
// If inReplyTo > 0, posts as a reply to that comment.
func (g *GHCLI) PostReviewComment(ctx context.Context, repo, number, commitSHA, path string, line int, side string, body string, inReplyTo int64) error {
	args := []string{"api", "--method", "POST",
		fmt.Sprintf("repos/%s/pulls/%s/comments", repo, number),
		"-f", "body=" + body,
		"-f", "commit_id=" + commitSHA,
		"-f", "path=" + path,
		"-f", fmt.Sprintf("line=%d", line),
		"-f", "side=" + strings.ToUpper(side),
	}
	if inReplyTo > 0 {
		args = append(args, "-f", fmt.Sprintf("in_reply_to=%d", inReplyTo))
	}
	_, err := g.run(ctx, args...)
	return err
}

// AuthToken returns gh's stored OAuth token (push credentials).
func (g *GHCLI) AuthToken(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "auth", "token")
	if err != nil {
		return "", fmt.Errorf("review: gh auth token: %w", err)
	}
	tok := strings.TrimSpace(out)
	if tok == "" {
		return "", fmt.Errorf("review: gh auth token: empty")
	}
	return tok, nil
}

// CreatePR opens a pull request and returns its metadata.
func (g *GHCLI) CreatePR(ctx context.Context, repo, title, body, base, head string) (PRMeta, error) {
	out, err := g.run(ctx, "pr", "create",
		"--repo", repo, "--title", title, "--body", body,
		"--base", base, "--head", head,
		"--json", "number,title,url,baseRefName,headRefName,headRefOid")
	if err != nil {
		return PRMeta{}, fmt.Errorf("review: gh pr create: %w", err)
	}
	var raw struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		BaseRefName string `json:"baseRefName"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return PRMeta{}, fmt.Errorf("review: gh pr create: decode: %w", err)
	}
	return PRMeta{Number: raw.Number, Title: raw.Title, URL: raw.URL, BaseRef: raw.BaseRefName}, nil
}

// ReviewComments fetches the PR's line-anchored review comments.
func (g *GHCLI) ReviewComments(ctx context.Context, repo, number string) ([]RemoteComment, error) {
	out, err := g.run(ctx, "api",
		fmt.Sprintf("repos/%s/pulls/%s/comments", repo, number),
		"--paginate")
	if err != nil {
		return nil, fmt.Errorf("review: gh api review comments: %w", err)
	}
	return parseReviewComments(out)
}

type ghRC struct {
	ID           int64  `json:"id"`
	InReplyTo    int64  `json:"in_reply_to_id"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"original_line"`
	Side         string `json:"side"`
	Body         string `json:"body"`
	User         struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

func parseReviewComments(data string) ([]RemoteComment, error) {
	var raw []ghRC
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, fmt.Errorf("review: gh api review comments: decode: %w", err)
	}
	byID := map[int64]ghRC{}
	for _, c := range raw {
		byID[c.ID] = c
	}
	out := make([]RemoteComment, 0, len(raw))
	for _, c := range raw {
		root := c.ID
		for cur := c.InReplyTo; cur != 0; {
			parent, ok := byID[cur]
			if !ok {
				break // parent beyond first page boundary: treat as root
			}
			root = parent.ID
			cur = parent.InReplyTo
		}
		rc := RemoteComment{
			ID: c.ID, RootID: root, Path: c.Path, Side: strings.ToUpper(c.Side),
			Body: c.Body, Author: c.User.Login, CreatedAt: c.CreatedAt,
		}
		if c.Line != nil {
			rc.Line = *c.Line
		}
		if c.OriginalLine != nil {
			rc.OrigLine = *c.OriginalLine
		}
		out = append(out, rc)
	}
	return out, nil
}

// IssueComments fetches the PR conversation-level comments.
func (g *GHCLI) IssueComments(ctx context.Context, repo, number string) ([]RemoteComment, error) {
	out, err := g.run(ctx, "api",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, number),
		"--paginate")
	if err != nil {
		return nil, fmt.Errorf("review: gh api issue comments: %w", err)
	}
	var raw []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("review: gh api issue comments: decode: %w", err)
	}
	rcs := make([]RemoteComment, 0, len(raw))
	for _, c := range raw {
		rcs = append(rcs, RemoteComment{
			ID: c.ID, RootID: c.ID, Body: c.Body,
			Author: c.User.Login, CreatedAt: c.CreatedAt,
		})
	}
	return rcs, nil
}
