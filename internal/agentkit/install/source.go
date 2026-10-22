// Package install puts skills and MCP servers into a workspace or into
// every workspace, from the places they are published (ADR-0043):
//
//   - a plugin from a marketplace (.claude-plugin/marketplace.json —
//     anthropics/claude-plugins-official carries the vendors' own
//     Atlassian, Slack, Zoom, Linear, Notion and GitHub plugins);
//   - a skill (or a repository of skills) by GitHub URL, any git URL, or
//     a local path;
//   - an MCP server by URL or by a pasted mcpServers snippet.
//
// Every install is two steps: Preview fetches and shows exactly what
// would land (skills, their scripts, servers and their commands), then
// Apply writes it. Sources are pinned to the commit fetched, recorded in
// installed.toml, and nothing third-party is ever modified.
package install

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Source is where an install comes from.
type Source struct {
	URL    string // git URL ("" for a local path)
	Path   string // local directory ("" for git)
	Ref    string // branch, tag or commit ("" = default branch)
	Subdir string // directory inside the repository ("" = root)
}

// String is the source as the user would type it again.
func (s Source) String() string {
	if s.Path != "" {
		return s.Path
	}
	out := s.URL
	if s.Ref != "" || s.Subdir != "" {
		out += "#" + s.Ref
		if s.Subdir != "" {
			out += ":" + s.Subdir
		}
	}
	return out
}

// ParseSource reads what a user pastes:
//
//	owner/repo                                    (GitHub)
//	github.com/owner/repo
//	https://github.com/owner/repo[.git]
//	https://github.com/owner/repo/tree/<ref>/<dir> (a folder on GitHub)
//	https://host/any.git, git@host:owner/repo.git (any git server)
//	<url>#<ref>[:<dir>]                           (explicit ref and folder)
//	./dir, ~/dir, /abs/dir                        (a local folder)
func ParseSource(raw string) (Source, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Source{}, fmt.Errorf("paste a GitHub URL, owner/repo, a git URL or a folder path")
	}
	if p, ok := localPath(raw); ok {
		fi, err := os.Stat(p)
		if err != nil {
			return Source{}, fmt.Errorf("%s: %w", raw, err)
		}
		if !fi.IsDir() {
			p = filepath.Dir(p) // a SKILL.md path means its folder
		}
		return Source{Path: p}, nil
	}
	var src Source
	if base, frag, ok := strings.Cut(raw, "#"); ok {
		raw = base
		src.Ref, src.Subdir, _ = strings.Cut(frag, ":")
	}
	switch {
	case strings.HasPrefix(raw, "git@"), strings.HasPrefix(raw, "ssh://"):
		src.URL = raw
		return src, nil
	case strings.HasPrefix(raw, "github.com/"):
		raw = "https://" + raw
	case !strings.Contains(raw, "://") && strings.Count(raw, "/") == 1 && !strings.HasPrefix(raw, "."):
		raw = "https://github.com/" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Source{}, fmt.Errorf("%q is not a GitHub URL, owner/repo, git URL or folder", raw)
	}
	if u.Host == "github.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return Source{}, fmt.Errorf("%s: want github.com/<owner>/<repo>", raw)
		}
		repo := strings.TrimSuffix(parts[1], ".git")
		src.URL = "https://github.com/" + parts[0] + "/" + repo + ".git"
		// /tree/<ref>/<dir> and /blob/<ref>/<dir>/SKILL.md name a folder.
		if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
			src.Ref = parts[3]
			dir := strings.Join(parts[4:], "/")
			if parts[2] == "blob" {
				dir = strings.TrimSuffix(strings.TrimSuffix(dir, "SKILL.md"), "/")
			}
			src.Subdir = dir
		}
		return src, nil
	}
	src.URL = raw
	return src, nil
}

// localPath reports raw as a local directory path.
func localPath(raw string) (string, bool) {
	switch {
	case strings.HasPrefix(raw, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return filepath.Join(home, raw[2:]), true
	case strings.HasPrefix(raw, "/"), strings.HasPrefix(raw, "./"), strings.HasPrefix(raw, "../"):
		abs, err := filepath.Abs(raw)
		return abs, err == nil
	case strings.HasPrefix(raw, "file://"):
		return strings.TrimPrefix(raw, "file://"), true
	}
	return "", false
}
