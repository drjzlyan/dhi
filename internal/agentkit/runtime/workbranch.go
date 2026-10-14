package runtime

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/workspace"
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
	// Count the continuation before it is visible: whoever sees the post
	// and waits on WaitBackground must find it already tracked.
	r.bg.Add(1)
	posted, err := r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel, Thread: bus.ThreadOf(trigger),
		Author: systemAuthor, Text: "@" + agentID + " continue: " + why,
	})
	if err != nil {
		r.bg.Done()
		return
	}
	go func() {
		defer r.bg.Done()
		r.Handle(context.WithoutCancel(ctx), posted)
	}()
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
	// What was already dirty when the turn began is not the agent's to
	// move: your own half-done edit, or another agent's (M32 review).
	pre := map[string]map[string]bool{}
	for _, m := range r.cfg.WS.Members() {
		pre[m.Name] = r.dirtyPaths(context.Background(), m.Path)
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
		stashed, left := false, []string(nil)
		if carry {
			if stashed, left, err = r.stashChanges(ctx, member, pre[member]); err != nil {
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
		if len(left) > 0 {
			note += fmt.Sprintf(" %d file(s) were already changed before this turn and stay in the shared checkout (%s); if you edited them, redo those edits in the worktree.",
				len(left), strings.Join(left, ", "))
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

// stashChanges stashes the files that became dirty during this turn
// (tracked and untracked, never .dhi/) — not what was dirty before it
// began (pre). It reports whether anything was stashed and the
// pre-dirty files it left alone.
func (r *Runtime) stashChanges(ctx context.Context, member string, pre map[string]bool) (bool, []string, error) {
	if r.cfg.Git == nil {
		return false, nil, fmt.Errorf("git unavailable")
	}
	dir := r.memberPath(member)
	var carry, left []string
	for p := range r.dirtyPaths(ctx, dir) {
		if pre[p] {
			left = append(left, p)
		} else {
			carry = append(carry, p)
		}
	}
	sort.Strings(carry)
	sort.Strings(left)
	if len(carry) == 0 {
		return false, left, nil
	}
	args := append([]string{"stash", "push", "--include-untracked", "-m", "dhi: work_branch carry", "--"}, carry...)
	if _, _, err := r.cfg.Git.Run(ctx, dir, args...); err != nil {
		return false, left, err
	}
	return true, left, nil
}

// dirtyPaths is the set of changed or untracked paths in dir, outside
// .dhi/ (nil when git cannot say).
func (r *Runtime) dirtyPaths(ctx context.Context, dir string) map[string]bool {
	if r.cfg.Git == nil || dir == "" {
		return nil
	}
	out, _, err := r.cfg.Git.Run(ctx, dir, "status", "--porcelain", "-z", "--untracked-files=all", "--", ".", ":(exclude).dhi")
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		set[f[3:]] = true
		if f[0] == 'R' || f[0] == 'C' {
			i++ // the rename's source path follows
			if i < len(fields) && fields[i] != "" {
				set[fields[i]] = true
			}
		}
	}
	return set
}

func (r *Runtime) popChanges(ctx context.Context, dir string) error {
	_, _, err := r.cfg.Git.Run(ctx, dir, "stash", "pop")
	return err
}

// CleanTmp removes what agents left in <root>/.dhi/tmp that nothing has
// touched for maxAge (F-086): the private temp dirs only ever grow.
// Recent entries stay — another DHI on the same workspace may be using
// them. It returns how many entries were removed.
func CleanTmp(root string, maxAge time.Duration, now time.Time) int {
	base := filepath.Join(root, workspace.DirTmp)
	agents, err := os.ReadDir(base)
	if err != nil {
		return 0
	}
	n := 0
	for _, a := range agents {
		dir := filepath.Join(base, a.Name())
		if !a.IsDir() {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if newestUnder(p).Before(now.Add(-maxAge)) {
				if os.RemoveAll(p) == nil {
					n++
				}
			}
		}
	}
	return n
}

// newestUnder is the latest modification time at or below p.
func newestUnder(p string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
