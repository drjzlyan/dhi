package sandbox

import "github.com/drjzlyan/dhi/internal/gitcore"

// DefaultRefsForTest mirrors an approved default-ref repair, then the guard.
func DefaultRefsForTest(repo string) []string {
	_ = gitcore.LooseDefaultRefs(repo)
	return gitcore.DefaultBranchRefs(repo)
}
