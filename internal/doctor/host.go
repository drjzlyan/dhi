package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/hostenv"
	"github.com/drjzlyan/dhi/internal/hosttools"
)

// The host suites (ADR-0044): DHI uses the user's own environment and
// tools, so doctor reports what it found there and, for what is missing,
// the exact command the user's package manager installs it with.

// Environment reports how the user's shell environment was read (F-109).
func Environment() []Check {
	s := hostenv.Load()
	if s.Err != nil {
		return []Check{{Name: "host/environment", Status: Warn,
			Detail: "using DHI's own environment: " + s.Err.Error() + " — tools your shell profile adds to PATH may be missing"}}
	}
	path, _ := hostenv.Lookup(s.Env, "PATH")
	return []Check{{Name: "host/environment", Status: OK,
		Detail: fmt.Sprintf("read from %s in %s (%d PATH entries)", s.Shell, s.Took.Round(time.Millisecond), len(strings.Split(path, string(os.PathListSeparator))))}}
}

// coreTools are the tools whole features depend on; the rest (language
// servers, debuggers, project toolchains) are offered where they are used.
var coreTools = []string{"git", "rg", "gh"}

// Tools reports the host tools DHI uses (F-110).
func Tools() []Check {
	var out []Check
	if m := hosttools.Managers(); len(m) > 0 {
		out = append(out, Check{Name: "host/installers", Status: OK, Detail: strings.Join(m, ", ")})
	} else {
		out = append(out, Check{Name: "host/installers", Status: Warn,
			Detail: "no package manager found (Homebrew, apt, dnf, pacman, zypper) — missing tools show their docs instead"})
	}
	for _, name := range coreTools {
		t, _ := hosttools.Get(name)
		if p, ok := hosttools.Find(t); ok {
			detail := p
			if v := hosttools.Version(t); v != "" {
				detail = v + " · " + p
			}
			out = append(out, Check{Name: "host/" + name, Status: OK, Detail: detail})
			continue
		}
		out = append(out, Check{Name: "host/" + name, Status: Warn,
			Detail: "not installed — DHI uses it for " + t.Purpose + "; install: " + hosttools.Missing(t)})
	}
	var have, missing []string
	for _, t := range hosttools.Tools {
		if isCore(t.Name) || !t.ForThisOS(runtime.GOOS) {
			continue
		}
		if _, ok := hosttools.Find(t); ok {
			have = append(have, t.Name)
		} else {
			missing = append(missing, t.Name)
		}
	}
	detail := "installed: " + orNone(have)
	if len(missing) > 0 {
		detail += " · not installed (offered where needed): " + strings.Join(missing, ", ")
	}
	out = append(out, Check{Name: "host/other-tools", Status: OK, Detail: detail})
	return out
}

func isCore(name string) bool {
	for _, c := range coreTools {
		if c == name {
			return true
		}
	}
	return false
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return strings.Join(xs, ", ")
}

// LegacyToolchain names the toolchain folder DHI used to keep, which it
// no longer reads; the repair at the next launch offers to delete it.
func LegacyToolchain() []Check {
	dir := hosttools.LegacyToolchainDir()
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	return []Check{{Name: "host/old-toolchain", Status: Warn,
		Detail: fmt.Sprintf("%s (%d MB) is no longer used — DHI offers to delete it at the next launch",
			dir, hosttools.DirSize(dir)>>20)}}
}

// userGit is the git doctor reports on: the user's own.
func userGit() *gitcore.Runner {
	r, err := gitcore.ResolveRunner(lookPath, os.Environ())
	if err != nil {
		return nil
	}
	return r
}

// Git reports the user's git (F-112): its version, or how to get it.
func Git() []Check {
	r := userGit()
	if r == nil {
		t, _ := hosttools.Get("git")
		return []Check{{Name: "git/version", Status: Warn,
			Detail: "git is not installed — commits, branches and reviews need it; install: " + hosttools.Missing(t)}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := r.Version(ctx)
	if err != nil {
		return []Check{{Name: "git/version", Status: Fail, Detail: strings.TrimSpace(err.Error())}}
	}
	return []Check{{Name: "git/version", Status: OK, Detail: "git " + v}}
}

// Identity reports the user's git identity (F-029): OK when user.name
// and user.email resolve from their config, Warn (naming the fix) when
// unset. Silent without git — the Git suite covers that. Unset is a
// Warn, not a Fail: the IDE boots and agents run; commit paths refuse at
// use with the same message.
func Identity() []Check {
	r := userGit()
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := gitcore.ResolveIdentity(ctx, r)
	if err != nil {
		return []Check{{Name: "identity", Status: Warn, Detail: err.Error()}}
	}
	return []Check{{Name: "identity", Status: OK, Detail: fmt.Sprintf("%s <%s>", id.Name, id.Email)}}
}

// GH reports the user's gh, which PR flows use with their login.
func GH() []Check {
	p, err := lookPath("gh")
	if err != nil {
		t, _ := hosttools.Get("gh")
		return []Check{{Name: "gh/cli", Status: Warn,
			Detail: "gh is not installed — pull requests need it; install: " + hosttools.Missing(t) + ", then `gh auth login`"}}
	}
	return []Check{{Name: "gh/cli", Status: OK, Detail: p}}
}

// sshAgentKeys counts the keys the user's ssh-agent holds (-1: no agent).
// Swapped by tests.
var sshAgentKeys = func() int {
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		return -1
	}
	ssh, err := lookPath("ssh-add")
	if err != nil {
		return -1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ssh, "-l").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return 0 // agent running, no identities
		}
		return -1
	}
	return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
}

// GitAccess reports how the user's git reaches remotes (F-112), names
// only: where the identity comes from, the credential helpers, and the
// ssh-agent. DHI never reads keys or tokens.
func GitAccess(wsRoot string) []Check {
	r := userGit()
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := wsRoot
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = ""
	}
	var parts []string
	if out, _, err := r.Run(ctx, dir, "config", "--show-origin", "--get", "user.email"); err == nil {
		origin, _, _ := strings.Cut(strings.TrimSpace(out), "\t")
		parts = append(parts, "identity from "+tilde(strings.TrimPrefix(origin, "file:")))
	}
	if out, _, err := r.Run(ctx, dir, "config", "--get-all", "credential.helper"); err == nil && strings.TrimSpace(out) != "" {
		var hs []string
		for _, h := range strings.Split(strings.TrimSpace(out), "\n") {
			if f := strings.Fields(h); len(f) > 0 {
				hs = append(hs, f[0])
			}
		}
		parts = append(parts, "credential helper "+strings.Join(hs, ", "))
	} else {
		parts = append(parts, "no credential helper (https remotes need one, e.g. `gh auth setup-git`)")
	}
	switch n := sshAgentKeys(); {
	case n < 0:
		parts = append(parts, "no ssh-agent")
	case n == 0:
		parts = append(parts, "ssh-agent with no keys loaded")
	default:
		parts = append(parts, fmt.Sprintf("ssh-agent with %d key(s)", n))
	}
	return []Check{{Name: "git/access", Status: OK, Detail: strings.Join(parts, " · ")}}
}

// tilde shows a path under home as ~/…
func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}
