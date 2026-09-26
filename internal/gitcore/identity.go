package gitcore

import (
	"context"
	"errors"
	"strings"
)

// Identity is the resolved git author identity (user.name/user.email).
// F-029: the user's identity is the only one that crosses outward.
type Identity struct {
	Name  string
	Email string
}

// ErrIdentityUnset is the named refusal for a missing git identity
// (ADR-0011): it states the exact fix, and the same message is used by
// commit paths and the doctor identity row.
var ErrIdentityUnset = errors.New(
	"git identity unset: run `git config --global user.name \"Your Name\"` and `git config --global user.email \"you@example.com\"`")

// IdentityFunc resolves the user's git identity on demand. Consumers
// inject one; a nil resolver makes commit paths refuse by name.
type IdentityFunc func(ctx context.Context) (Identity, error)

// ResolveIdentity reads user.name/user.email through the hermetic git
// binary under the HOST config path (HOME / user global config). It
// deliberately does NOT use the managed hermetic config (ADR-0009),
// which carries no [user] section: reading the user's identity is the
// named exception that makes everything reaching a remote belong to the
// user (F-029).
func ResolveIdentity(ctx context.Context, r *Runner) (Identity, error) {
	name := r.configGet(ctx, "user.name")
	email := r.configGet(ctx, "user.email")
	if name == "" || email == "" {
		return Identity{}, ErrIdentityUnset
	}
	return Identity{Name: name, Email: email}, nil
}

// configGet returns the trimmed value of a git config key, or "" when
// unset (git exits non-zero; Run reports an error we treat as unset,
// never as a silent default).
func (r *Runner) configGet(ctx context.Context, key string) string {
	out, _, err := r.Run(ctx, "", "config", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
