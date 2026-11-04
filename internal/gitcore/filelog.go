package gitcore

import (
	"context"
	"strconv"
	"strings"
)

// FileCommit is one commit that touched a file (F-140).
type FileCommit struct {
	Hash    string // full
	Short   string
	When    string // relative commit time, "2 hours ago"
	Author  string
	Subject string
	Path    string // the file's path in that commit, repo-relative (differs across renames)
}

// FileLog lists the commits that touched rel (a path relative to dir),
// newest first, following renames, with the user's git.
func FileLog(ctx context.Context, r *Runner, dir, rel string, limit int) ([]FileCommit, error) {
	args := []string{"log", "--follow", "--no-color", "--name-only",
		"--format=%x1e%H%x1f%h%x1f%cr%x1f%an%x1f%s"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, _, err := r.Run(ctx, dir, append(args, "--", rel)...)
	if err != nil {
		return nil, err
	}
	var res []FileCommit
	for _, rec := range strings.Split(out, "\x1e") {
		head, files, _ := strings.Cut(rec, "\n")
		f := strings.Split(head, "\x1f")
		if len(f) != 5 {
			continue
		}
		c := FileCommit{Hash: f[0], Short: f[1], When: f[2], Author: f[3], Subject: f[4]}
		for _, p := range strings.Split(files, "\n") {
			if p = strings.TrimSpace(p); p != "" {
				c.Path = p
				break
			}
		}
		res = append(res, c)
	}
	return res, nil
}
