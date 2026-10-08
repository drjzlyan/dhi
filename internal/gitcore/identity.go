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

// IdentityCommands returns the exact `git config --global` invocations
// SetIdentity runs, for display before the user confirms (F-043).
func IdentityCommands(id Identity) []string {
	return []string{
		"git config --global user.name " + shellQuote(id.Name),
		"git config --global user.email " + shellQuote(id.Email),
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ValidateIdentity rejects values git would store but commits would
// misattribute: empty parts, control characters, an email with no '@'.
func ValidateIdentity(id Identity) error {
	name, email := strings.TrimSpace(id.Name), strings.TrimSpace(id.Email)
	if name == "" {
		return errors.New("name is required")
	}
	if email == "" || !strings.Contains(email, "@") || strings.ContainsAny(email, " <>") {
		return errors.New("email must look like you@example.com")
	}
	for _, r := range name + email {
		if r < 0x20 || r == 0x7f {
			return errors.New("name and email cannot contain control characters")
		}
	}
	return nil
}

// SetIdentity writes user.name/user.email to the user's GLOBAL git config.
// The Runner must carry the identity environment (host HOME, no forced
// GIT_CONFIG_GLOBAL — toolchain.GitIdentityEnv), the same named exception
// ResolveIdentity uses; callers confirm with the user before calling.
func SetIdentity(ctx context.Context, r *Runner, id Identity) error {
	if err := ValidateIdentity(id); err != nil {
		return err
	}
	id = Identity{Name: strings.TrimSpace(id.Name), Email: strings.TrimSpace(id.Email)}
	if _, _, err := r.Run(ctx, "", "config", "--global", "user.name", id.Name); err != nil {
		return err
	}
	_, _, err := r.Run(ctx, "", "config", "--global", "user.email", id.Email)
	return err
}
