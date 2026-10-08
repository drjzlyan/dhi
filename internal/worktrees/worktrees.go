// Package worktrees decides where DHI's linked git worktrees live (F-053).
//
// By default they sit inside the workspace (.dhi/tasks/<slug>/<member>,
// .dhi/reviews/<id>/<member>). A user can move them elsewhere — a faster disk,
// a folder a cloud-sync client does not watch — with `worktrees.root` in
// settings. Cards store worktree paths relative to the workspace root, so an
// external location is stored as a relative path that may climb out of the
// workspace ("../x/..."); every consumer already joins it onto the root.
package worktrees

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Defaults: the in-workspace layout, unchanged from before the setting existed.
const (
	TasksDir   = ".dhi/tasks"
	ReviewsDir = ".dhi/reviews"
)

// Layout maps tasks and reviews to worktree paths.
type Layout struct {
	wsRoot string
	base   string // absolute external base ("" = in-workspace defaults)
}

// Resolve builds the layout for a workspace. setting is the user's
// worktrees.root: "" keeps the defaults; "~" or "~/x" expands against home;
// a relative path is taken from the workspace root; the result must be absolute
// and expressible relative to the workspace root (same volume).
//
// A shared root is namespaced per workspace — "<name>-<hash of the root path>" —
// so two workspaces never collide on a task slug.
func Resolve(wsRoot, setting, home string) (Layout, error) {
	l := Layout{wsRoot: wsRoot}
	setting = strings.TrimSpace(setting)
	if setting == "" {
		return l, nil
	}
	p := setting
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		if home == "" {
			return Layout{}, fmt.Errorf("worktrees.root %q needs a home directory, which is unknown", setting)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	case !filepath.IsAbs(p):
		p = filepath.Join(wsRoot, p)
	}
	p = filepath.Clean(p)
	sum := sha256.Sum256([]byte(filepath.Clean(wsRoot)))
	name := filepath.Base(filepath.Clean(wsRoot))
	l.base = filepath.Join(p, name+"-"+hex.EncodeToString(sum[:3]))
	if _, err := filepath.Rel(wsRoot, l.base); err != nil {
		return Layout{}, fmt.Errorf("worktrees.root %q cannot be reached from the workspace (%v)", setting, err)
	}
	return l, nil
}

// External reports whether worktrees live outside the workspace's .dhi folder.
func (l Layout) External() bool { return l.base != "" }

// Base is the absolute folder holding all worktrees ("" = defaults).
func (l Layout) Base() string { return l.base }

// Ensure creates the external base so the sandbox and the path jail can
// canonicalize it. A no-op for the default layout.
func (l Layout) Ensure() error {
	if l.base == "" {
		return nil
	}
	if err := os.MkdirAll(l.base, 0o755); err != nil {
		return fmt.Errorf("worktrees: create %s: %w", l.base, err)
	}
	return nil
}

// Task returns a task worktree's path relative to the workspace root (what
// ccards store) and absolute.
func (l Layout) Task(slug, member string) (rel, abs string) {
	return l.path("tasks", TasksDir, slug, member)
}

// Review returns a review worktree's relative and absolute path.
func (l Layout) Review(id, member string) (rel, abs string) {
	return l.path("reviews", ReviewsDir, id, member)
}

func (l Layout) path(kind, def, id, member string) (rel, abs string) {
	if l.base == "" {
		rel = filepath.Join(def, id, member)
		return rel, filepath.Join(l.wsRoot, rel)
	}
	abs = filepath.Join(l.base, kind, id, member)
	rel, err := filepath.Rel(l.wsRoot, abs)
	if err != nil { // Resolve checked this; keep the absolute path usable
		return abs, abs
	}
	return rel, abs
}
