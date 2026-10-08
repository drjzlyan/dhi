package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	// SubmitReview sends ONE review (verdict + summary + inline comments)
	// as the authenticated user and returns its URL.
	SubmitReview(ctx context.Context, repo, number string, sub ReviewSubmission) (string, error)
	// Login is the authenticated user's login ("" if unknown).
	Login(ctx context.Context) (string, error)
	ReviewComments(ctx context.Context, repo, number string) ([]RemoteComment, error)
	IssueComments(ctx context.Context, repo, number string) ([]RemoteComment, error)
}

// Review verdicts (GitHub review events).
const (
	EventComment        = "COMMENT"
	EventApprove        = "APPROVE"
	EventRequestChanges = "REQUEST_CHANGES"
)

// ReviewCommentInput is one inline comment inside a submitted review.
// Line 0 anchors the comment to the whole file.
type ReviewCommentInput struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Side string `json:"side,omitempty"` // "RIGHT" | "LEFT"
	Body string `json:"body"`
	// SubjectType is "file" for a file-level comment, else omitted.
	SubjectType string `json:"subject_type,omitempty"`
}

// ReviewSubmission is the payload of one GitHub review.
type ReviewSubmission struct {
	CommitSHA string               `json:"commit_id,omitempty"`
	Event     string               `json:"event"`
	Body      string               `json:"body,omitempty"`
	Comments  []ReviewCommentInput `json:"comments,omitempty"`
}

// ownerRepo reduces any GitHub remote form to host and "owner/name":
//
//	https://github.com/acme/api.git   git@github.com:acme/api.git
//	ssh://git@github.com/acme/api     github.com/acme/api     acme/api
//
// `gh api repos/<x>/…` needs the bare owner/name — handing it a remote URL
// fails with "unsupported protocol scheme". The host is "" for github.com
// and for the bare form.
func ownerRepo(remote string) (host, repo string) {
	s := strings.TrimSpace(remote)
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	if i := strings.Index(s, "://"); i >= 0 { // scheme://[user@]host[:port]/path
		s = s[i+3:]
		if at := strings.LastIndex(s, "@"); at >= 0 && at < strings.Index(s, "/") {
			s = s[at+1:]
		}
	} else if at := strings.Index(s, "@"); at >= 0 { // scp form git@host:owner/name
		s = strings.Replace(s[at+1:], ":", "/", 1)
	}
	parts := strings.Split(s, "/")
	if len(parts) >= 3 { // host/owner/name[/…]
		host = parts[0]
		if i := strings.Index(host, ":"); i >= 0 {
			host = host[:i]
		}
		if host == "github.com" {
			host = ""
		}
		return host, parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return "", s
}

// apiArgs builds `gh api` args for repo-scoped paths.
func apiArgs(repo, path string, extra ...string) []string {
	host, or := ownerRepo(repo)
	args := []string{"api"}
	if host != "" {
		args = append(args, "--hostname", host)
	}
	args = append(args, extra...)
	return append(args, fmt.Sprintf("repos/%s/%s", or, path))
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

// GHCLI runs DHI's hermetic gh shim (registry-pinned, ADR-0011). The
// host `gh` lookup path is gone: when the shim is absent the seam
// reports unavailable and PR flows refuse with the named fix.
type GHCLI struct {
	bin string // shim path; "" = unavailable
}

// NewGHCLI binds the gh seam to a shim path. Callers pass the
// toolchain shim path (<prefix>/bin/gh); an empty path yields a
// permanently unavailable seam.
func NewGHCLI(bin string) *GHCLI { return &GHCLI{bin: bin} }

// Available reports whether the hermetic gh shim was provided.
func (g *GHCLI) Available() bool { return g.bin != "" }

func (g *GHCLI) run(ctx context.Context, args ...string) (string, error) {
	if g.bin == "" {
		return "", fmt.Errorf("review: gh shim not installed — run bootstrap (PR flows refuse until then, ADR-0011)")
	}
	c := exec.CommandContext(ctx, g.bin, args...)
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
	args := append(apiArgs(repo, fmt.Sprintf("pulls/%s/comments", number), "--method", "POST"),
		"-f", "body="+body,
		"-f", "commit_id="+commitSHA,
		"-f", "path="+path,
		"-f", fmt.Sprintf("line=%d", line),
		"-f", "side="+strings.ToUpper(side))
	if inReplyTo > 0 {
		args = append(args, "-f", fmt.Sprintf("in_reply_to=%d", inReplyTo))
	}
	_, err := g.run(ctx, args...)
	return err
}

// SubmitReview sends one review: verdict, summary and inline comments in a
// single API call, so the PR shows ONE review by the authenticated user
// rather than a scatter of loose comments.
func (g *GHCLI) SubmitReview(ctx context.Context, repo, number string, sub ReviewSubmission) (string, error) {
	if sub.Event == "" {
		sub.Event = EventComment
	}
	payload, err := json.Marshal(sub)
	if err != nil {
		return "", fmt.Errorf("review: encode review: %w", err)
	}
	f, err := os.CreateTemp("", "dhi-review-*.json")
	if err != nil {
		return "", fmt.Errorf("review: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("review: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("review: %w", err)
	}
	out, err := g.run(ctx, append(apiArgs(repo, fmt.Sprintf("pulls/%s/reviews", number), "--method", "POST"),
		"--input", f.Name())...)
	if err != nil {
		return "", fmt.Errorf("review: submit review: %w", err)
	}
	var resp struct {
		HTMLURL string `json:"html_url"`
	}
	_ = json.Unmarshal([]byte(out), &resp) // the URL is a convenience; the review is already sent
	return resp.HTMLURL, nil
}

// Login returns the authenticated user's login.
func (g *GHCLI) Login(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("review: gh api user: %w", err)
	}
	return strings.TrimSpace(out), nil
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
	out, err := g.run(ctx, append(apiArgs(repo, fmt.Sprintf("pulls/%s/comments", number)), "--paginate")...)
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
	out, err := g.run(ctx, append(apiArgs(repo, fmt.Sprintf("issues/%s/comments", number)), "--paginate")...)
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
