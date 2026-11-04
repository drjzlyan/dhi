package gitcore

import (
	"context"
	"fmt"
	"strings"
)

// Stash is one entry of `git stash list` (F-129).
type Stash struct {
	Ref     string // stash@{0}
	Message string // "On main: wip" or "WIP on main: abc1234 subject"
	When    string // relative commit time, "2 hours ago"
}

// ErrNothingToStash is returned when the work tree has no changes git would save.
var ErrNothingToStash = fmt.Errorf("gitcore: no local changes to stash")

// Stashes lists the repository's stashes, newest first, with the user's git.
func Stashes(ctx context.Context, r *Runner, dir string) ([]Stash, error) {
	out, _, err := r.Run(ctx, dir, "stash", "list", "--format=%gd%x1f%gs%x1f%cr")
	if err != nil {
		return nil, err
	}
	var res []Stash
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(ln, "\x1f")
		if len(f) != 3 {
			continue
		}
		res = append(res, Stash{Ref: f[0], Message: f[1], When: f[2]})
	}
	return res, nil
}

// StashPush saves tracked changes (message optional) and resets the work
// tree. A clean tree is ErrNothingToStash, not a silent success.
func StashPush(ctx context.Context, r *Runner, dir, message string) error {
	args := []string{"stash", "push"}
	if m := strings.TrimSpace(message); m != "" {
		args = append(args, "-m", m)
	}
	out, _, err := r.Run(ctx, dir, args...)
	if err != nil {
		return err
	}
	if strings.Contains(out, "No local changes to save") {
		return ErrNothingToStash
	}
	return nil
}

// StashApply re-applies a stash and keeps it.
func StashApply(ctx context.Context, r *Runner, dir, ref string) error {
	return stashRef(ctx, r, dir, "apply", ref)
}

// StashPop applies a stash and drops it (git keeps it when the apply conflicts).
func StashPop(ctx context.Context, r *Runner, dir, ref string) error {
	return stashRef(ctx, r, dir, "pop", ref)
}

// StashDrop discards a stash. ref is a Ref value from Stashes.
func StashDrop(ctx context.Context, r *Runner, dir, ref string) error {
	return stashRef(ctx, r, dir, "drop", ref)
}

func stashRef(ctx context.Context, r *Runner, dir, verb, ref string) error {
	if !strings.HasPrefix(ref, "stash@{") {
		return fmt.Errorf("gitcore: %q is not a stash ref", ref)
	}
	_, _, err := r.Run(ctx, dir, "stash", verb, ref)
	return err
}
