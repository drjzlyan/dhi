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

// PackedOnlyDefaultRefs lists the default-branch ref files that exist
// only in packed-refs: a sandbox that guards existing files (bubblewrap
// binds read-only only what exists) cannot protect them until a loose
// copy is written. Git reads loose and packed refs alike.
func PackedOnlyDefaultRefs(repo string) []string {
	gitDir := filepath.Join(repo, ".git")
	packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return nil
	}
	var out []string
	for _, ref := range DefaultBranchRefs(repo) {
		if strings.HasSuffix(ref, ".lock") {
			continue
		}
		if _, err := os.Stat(ref); err == nil {
			continue
		}
		if packedSHA(string(packed), gitDir, ref) != "" {
			out = append(out, ref)
		}
	}
	return out
}

func packedSHA(packed, gitDir, ref string) string {
	rel, err := filepath.Rel(gitDir, ref)
	if err != nil {
		return ""
	}
	name := filepath.ToSlash(rel)
	for _, line := range strings.Split(packed, "\n") {
		sha, r, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && r == name && len(sha) >= 40 {
			return sha
		}
	}
	return ""
}

// LooseDefaultRefs writes a loose copy of each packed-only default ref
// (the same commit). It changes the repo, so callers ask first (F-112).
func LooseDefaultRefs(repo string) error {
	packed, err := os.ReadFile(filepath.Join(repo, ".git", "packed-refs"))
	if err != nil {
		return err
	}
	for _, ref := range PackedOnlyDefaultRefs(repo) {
		sha := packedSHA(string(packed), filepath.Join(repo, ".git"), ref)
		if err := os.MkdirAll(filepath.Dir(ref), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(ref, []byte(sha+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}
