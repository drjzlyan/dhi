// Package sandbox implements DHI's deny-by-default path-jail and
// permission-policy engine (ADR-0006). Agent-driven filesystem and
// process operations resolve through a Jail (registered workspace roots)
// and a Policy (op → effect rules). OS-level isolation stays behind the
// Sandbox interface so bubblewrap/seatbelt adapters can wrap execution
// later without touching call sites.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Jail constrains absolute paths to a set of registered workspace roots.
type Jail struct {
	roots []string
}

// NewJail registers roots (each absolute; cleaned and symlink-canonicalized
// so comparisons are stable even when ancestors are links, e.g. macOS
// /var → /private/var). At least one root is required.
func NewJail(roots ...string) (*Jail, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("sandbox: jail requires at least one root")
	}
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			return nil, fmt.Errorf("sandbox: root %q is not absolute", r)
		}
		resolved, err := filepath.EvalSymlinks(r)
		if err != nil {
			return nil, fmt.Errorf("sandbox: root %s: %w", r, err)
		}
		cleaned = append(cleaned, filepath.Clean(resolved))
	}
	return &Jail{roots: cleaned}, nil
}

// Roots returns the registered roots in registration order.
func (j *Jail) Roots() []string {
	return append([]string(nil), j.roots...)
}

// Contains reports whether path resolves inside one of the roots.
// Symlinks are resolved along the deepest existing ancestor so a link
// pointing outside the jail cannot masquerade as internal. The path need
// not exist.
func (j *Jail) Contains(path string) bool {
	resolved, ok := j.resolve(path)
	if !ok {
		return false
	}
	for _, root := range j.roots {
		if within(root, resolved) {
			return true
		}
	}
	return false
}

// Root resolves path to its registered root; ok is false when outside.
func (j *Jail) Root(path string) (root string, rel string, ok bool) {
	resolved, valid := j.resolve(path)
	if !valid {
		return "", "", false
	}
	for _, r := range j.roots {
		if within(r, resolved) {
			relPath, err := filepath.Rel(r, resolved)
			if err != nil {
				return "", "", false
			}
			return r, relPath, true
		}
	}
	return "", "", false
}

// resolve cleans and symlink-resolves path as far as it exists.
func (j *Jail) resolve(path string) (string, bool) {
	if !filepath.IsAbs(path) {
		return "", false
	}
	abs := filepath.Clean(path)
	prefix := abs
	for {
		resolved, err := filepath.EvalSymlinks(prefix)
		if err == nil {
			var rest string
			if prefix != abs {
				suffix, err := filepath.Rel(prefix, abs)
				if err != nil {
					return "", false
				}
				rest = string(os.PathSeparator) + suffix
			}
			return filepath.Clean(resolved + rest), true
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return "", false
		}
		prefix = parent
	}
}

func within(root, path string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(os.PathSeparator))
}
