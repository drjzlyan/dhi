package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
)

// relaunch is a turn-local request to continue the agent in a fresh run
// once the current spawn ends (F-086): a new worktree (work_branch) or a
// new sandbox grant only takes effect at the next spawn, because the
// CLI's cwd and the OS sandbox profile are fixed when it starts.
type relaunch struct {
	mu  sync.Mutex
	why string
}

func (c *relaunch) set(why string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.why = why
}

func (c *relaunch) get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.why
}

// continueTurn hands the thread back to the agent through a visible DHI
// post: the new turn resolves the card bound to the thread (and with it
// the worktree) and its grants at spawn.
func (r *Runtime) continueTurn(ctx context.Context, agentID string, trigger bus.Message, why string) {
	posted, err := r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel, Thread: bus.ThreadOf(trigger),
		Author: systemAuthor, Text: "@" + agentID + " continue: " + why,
	})
	if err != nil {
		return
	}
	r.goHandle(context.WithoutCancel(ctx), posted)
}

// workBranchSeam backs the work_branch tool (F-086): it gives a chat
// turn the card, convention branch and worktree a task turn gets, so
// code work never lands on the shared checkout's default branch. An
// existing slug the agent owns is resumed (its thread rebinds here).
// With carry, uncommitted changes in the member checkout move into the
// new worktree (stash, then pop there) — the yume-forge case, where the
// fixes were already made in place.
func (r *Runtime) workBranchSeam(agentID string, trigger bus.Message, rl *relaunch) func(ctx context.Context, slug, title, member string, carry bool) (string, error) {
	if r.cfg.Tasks == nil || r.cfg.Provision == nil {
		return nil
	}
	return func(ctx context.Context, slug, title, member string, carry bool) (string, error) {
		if cur, ok := r.boundTask(trigger); ok && len(cur.ChangeSets) > 0 {
			return fmt.Sprintf("this thread already works in %s on branch %s (task %s)",
				cur.ChangeSets[0].Path, cur.ChangeSets[0].Branch, cur.Slug), nil
		}
		member, err := r.workMember(member)
		if err != nil {
			return "", err
		}
		t, exists := r.cfg.Tasks.Get(slug)
		switch {
		case !exists:
			if err := r.cfg.Tasks.Create(slug, title, agentID, ""); err != nil {
				return "", err
			}
		case t.Assignee != agentID:
			return "", fmt.Errorf("task %s belongs to %s; pick another slug", slug, orNobody(t.Assignee))
		}
		if err := r.cfg.Tasks.BindThread(slug, trigger.Channel, bus.ThreadOf(trigger)); err != nil {
			return "", err
		}
		stashed := false
		if carry {
			if stashed, err = r.stashChanges(ctx, member); err != nil {
				return "", fmt.Errorf("could not carry your changes: %w", err)
			}
		}
		t, _ = r.cfg.Tasks.Get(slug)
		if len(t.ChangeSets) == 0 {
			if err := r.cfg.Provision(slug, member); err != nil {
				if stashed {
					r.popChanges(ctx, r.memberPath(member))
				}
				return "", fmt.Errorf("could not create the worktree: %w", err)
			}
			t, _ = r.cfg.Tasks.Get(slug)
		}
		if len(t.ChangeSets) == 0 {
			return "", fmt.Errorf("task %s has no worktree after provisioning", slug)
		}
		cs := t.ChangeSets[0]
		note := ""
		if stashed {
			if err := r.popChanges(ctx, r.taskWorktree(t)); err != nil {
				note = fmt.Sprintf(" Your changes are in the git stash (could not apply them there: %v).", err)
			} else {
				note = " Your uncommitted changes moved there."
			}
		}
		why := fmt.Sprintf("task %s now works in %s on branch %s.%s", slug, cs.Path, cs.Branch, note)
		rl.set(why)
		return why + " End this run now with one short line; DHI continues you in the worktree.", nil
	}
}

// workMember picks the member a chat turn's branch is cut from: the one
// named, or the only member.
func (r *Runtime) workMember(name string) (string, error) {
	members := r.cfg.WS.Members()
	if name != "" {
		for _, m := range members {
			if m.Name == name {
				return name, nil
			}
		}
		return "", fmt.Errorf("no member %q", name)
	}
	if len(members) != 1 {
		var names []string
		for _, m := range members {
			names = append(names, m.Name)
		}
		return "", fmt.Errorf("the workspace has %d members (%s): name the one to branch with member", len(members), strings.Join(names, ", "))
	}
	return members[0].Name, nil
}

func (r *Runtime) memberPath(name string) string {
	for _, m := range r.cfg.WS.Members() {
		if m.Name == name {
			return m.Path
		}
	}
	return ""
}

// stashChanges stashes the member checkout's uncommitted work (tracked
// and untracked), reporting whether anything was stashed.
func (r *Runtime) stashChanges(ctx context.Context, member string) (bool, error) {
	if r.cfg.Git == nil {
		return false, fmt.Errorf("git unavailable")
	}
	dir := r.memberPath(member)
	out, _, err := r.cfg.Git.Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(out) == "" {
		return false, nil
	}
	if _, _, err := r.cfg.Git.Run(ctx, dir, "stash", "push", "--include-untracked", "-m", "dhi: work_branch carry"); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runtime) popChanges(ctx context.Context, dir string) error {
	_, _, err := r.cfg.Git.Run(ctx, dir, "stash", "pop")
	return err
}
