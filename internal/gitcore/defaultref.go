package gitcore

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultBranchRefs lists the files that move a repo's default branch:
// refs/heads/<name> and the <name>.lock git takes before writing it, for
// main, master and the branch origin/HEAD names (F-086). A sandbox that
// denies writing them leaves agents free to commit on any other branch
// while the default one moves only through DHI or the human. A repo
// whose .git is not a directory (a linked worktree) yields nothing.
func DefaultBranchRefs(repo string) []string {
	gitDir := filepath.Join(repo, ".git")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		return nil
	}
	names := []string{"main", "master"}
	if b, err := os.ReadFile(filepath.Join(gitDir, "refs", "remotes", "origin", "HEAD")); err == nil {
		if ref, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/remotes/origin/"); ok && ref != "" {
			names = append(names, ref)
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		ref := filepath.Join(gitDir, "refs", "heads", filepath.FromSlash(n))
		out = append(out, ref, ref+".lock")
	}
	return out
}
