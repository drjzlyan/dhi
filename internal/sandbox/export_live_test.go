package sandbox

import "github.com/drjzlyan/dhi/internal/gitcore"

// DefaultRefsForTest mirrors guardWorkspace: materialize, then guard.
func DefaultRefsForTest(repo string) []string {
	gitcore.LooseDefaultRefs(repo)
	return gitcore.DefaultBranchRefs(repo)
}
