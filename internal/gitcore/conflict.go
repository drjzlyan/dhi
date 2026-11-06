package gitcore

import (
	"context"
	"strings"
)

// Conflicted is one unmerged path with its two-letter porcelain code (F-156).
type Conflicted struct {
	Path string
	Code string // UU, AA, DD, AU, UA, DU or UD
}

// Conflicts lists the unmerged paths of a merge, rebase, stash pop or
// cherry-pick, sorted as git reports them. go-git's status cannot tell a
// conflict from a change, so this asks the user's git (ADR-0044).
func Conflicts(ctx context.Context, r *Runner, dir string) ([]Conflicted, error) {
	out, _, err := r.Run(ctx, dir, "status", "--porcelain=v1", "-z")
	if err != nil {
		return nil, err
	}
	return parseConflicts(out), nil
}

func parseConflicts(out string) []Conflicted {
	var res []Conflicted
	f := strings.Split(out, "\x00")
	for i := 0; i < len(f); i++ {
		e := f[i]
		if len(e) < 4 {
			continue
		}
		code := e[:2]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // a rename or copy carries its source as the next field
			continue
		}
		switch code {
		case "UU", "AA", "DD", "AU", "UA", "DU", "UD":
			res = append(res, Conflicted{Path: e[3:], Code: code})
		}
	}
	return res
}

// StagePath marks a resolved file: `git add -- rel`.
func StagePath(ctx context.Context, r *Runner, dir, rel string) error {
	_, _, err := r.Run(ctx, dir, "add", "--", rel)
	return err
}
