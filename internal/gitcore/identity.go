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

// Suggestion is a candidate identity and where it was found (F-083).
type Suggestion struct {
	Identity
	Source string
}

// SuggestIdentity gathers candidate identities, best first: each repo's
// local config, then its most frequent human commit author, then extra
// (sources the caller resolved: GitHub, the OS account). Duplicates and
// bots are dropped. Nothing is written.
func SuggestIdentity(ctx context.Context, r *Runner, dirs []string, extra ...Suggestion) []Suggestion {
	var out []Suggestion
	seen := map[Identity]bool{}
	add := func(s Suggestion) {
		s.Name, s.Email = strings.TrimSpace(s.Name), strings.TrimSpace(s.Email)
		if (s.Name == "" && s.Email == "") || seen[s.Identity] || isBot(s.Identity) {
			return
		}
		seen[s.Identity] = true
		out = append(out, s)
	}
	if r != nil {
		for _, d := range dirs {
			name, _, _ := r.Run(ctx, d, "config", "--local", "--get", "user.name")
			email, _, _ := r.Run(ctx, d, "config", "--local", "--get", "user.email")
			add(Suggestion{Identity{strings.TrimSpace(name), strings.TrimSpace(email)}, "this repo's git config"})
		}
		for _, d := range dirs {
			if id, ok := topAuthor(ctx, r, d); ok {
				add(Suggestion{id, "most commits in this repo"})
			}
		}
	}
	for _, s := range extra {
		add(s)
	}
	return out
}

// BestSuggestion merges suggestions field by field: the first name and
// the first email found, with both sources named.
func BestSuggestion(sugs []Suggestion) Suggestion {
	var best Suggestion
	var nameFrom, emailFrom string
	for _, s := range sugs {
		if best.Name == "" && s.Name != "" {
			best.Name, nameFrom = s.Name, s.Source
		}
		if best.Email == "" && s.Email != "" {
			best.Email, emailFrom = s.Email, s.Source
		}
	}
	switch {
	case nameFrom != "" && nameFrom == emailFrom:
		best.Source = "from " + nameFrom
	case nameFrom != "" && emailFrom != "":
		best.Source = "name from " + nameFrom + ", email from " + emailFrom
	case nameFrom != "":
		best.Source = "name from " + nameFrom
	case emailFrom != "":
		best.Source = "email from " + emailFrom
	}
	return best
}

func isBot(id Identity) bool {
	l := strings.ToLower(id.Name + " " + id.Email)
	return strings.Contains(l, "[bot]") || strings.Contains(l, "dependabot") || strings.Contains(l, "github-actions") ||
		strings.HasPrefix(strings.ToLower(id.Email), "noreply@")
}

// topAuthor is the most frequent human author of dir's last 200 commits.
func topAuthor(ctx context.Context, r *Runner, dir string) (Identity, bool) {
	out, _, err := r.Run(ctx, dir, "log", "-n", "200", "--format=%an%x09%ae")
	if err != nil {
		return Identity{}, false
	}
	counts := map[Identity]int{}
	var order []Identity
	for _, l := range strings.Split(out, "\n") {
		name, email, ok := strings.Cut(strings.TrimSpace(l), "\t")
		if !ok {
			continue
		}
		id := Identity{strings.TrimSpace(name), strings.TrimSpace(email)}
		if isBot(id) {
			continue
		}
		if counts[id] == 0 {
			order = append(order, id)
		}
		counts[id]++
	}
	var best Identity
	for _, id := range order { // first seen (newest) wins a tie
		if counts[id] > counts[best] {
			best = id
		}
	}
	return best, best != Identity{}
}
