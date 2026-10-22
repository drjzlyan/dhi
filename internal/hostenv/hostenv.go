// Package hostenv reads the user's environment the way their own shell
// sees it (F-109, ADR-0044). A TUI started from a bare terminal, a
// launcher or a service would otherwise miss what the user's rc files set
// up: nvm, asdf, mise, pyenv, Homebrew's PATH. Like VS Code's shell
// environment resolution, DHI runs the login shell once with a timeout,
// reads its environment, and starts every child process from it.
package hostenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Timeout bounds the shell probe; a slow rc file never delays launch more.
const Timeout = 3 * time.Second

// marker separates rc-file noise from the environment dump.
const marker = "__DHI_ENV_BEGIN__"

// Snapshot is a resolved environment and how it was obtained.
type Snapshot struct {
	Env    []string      // KEY=value, as for exec.Cmd.Env
	Shell  string        // the shell probed ("" when not probed)
	Took   time.Duration // how long the probe took
	Source string        // "login shell" or "DHI's own environment"
	Err    error         // why the probe was not used, if it was not
}

var (
	mu   sync.RWMutex
	snap *Snapshot
	// probe is swapped by tests.
	probe = probeShell
)

// Load resolves the environment (once; Reload refreshes it).
func Load() Snapshot {
	mu.RLock()
	s := snap
	mu.RUnlock()
	if s != nil {
		return *s
	}
	return Reload()
}

// Reload reads the login shell's environment again.
func Reload() Snapshot {
	s := resolve()
	mu.Lock()
	snap = &s
	mu.Unlock()
	return s
}

// Set installs a snapshot (tests, and callers that resolved it already).
func Set(s Snapshot) {
	mu.Lock()
	snap = &s
	mu.Unlock()
}

// Env returns a copy of the resolved environment.
func Env() []string { return append([]string(nil), Load().Env...) }

// Get returns one variable from the resolved environment.
func Get(key string) string {
	v, _ := Lookup(Load().Env, key)
	return v
}

// Lookup finds key in env.
func Lookup(env []string, key string) (string, bool) {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- { // the last assignment wins, as in exec
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):], true
		}
	}
	return "", false
}

// With returns env with key set to value (replacing any earlier value).
func With(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	prefix := key + "="
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return append(out, prefix+value)
}

// LookPath resolves name on the resolved PATH (an absolute or relative
// path with a separator is checked as is).
func LookPath(name string) (string, error) {
	return LookPathIn(Load().Env, name)
}

// LookPathIn resolves name on env's PATH.
func LookPathIn(env []string, name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		if isExec(name) {
			return name, nil
		}
		return "", fmt.Errorf("%s: not an executable file", name)
	}
	path, _ := Lookup(env, "PATH")
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if isExec(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: %w", name, exec.ErrNotFound)
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func resolve() Snapshot {
	base := os.Environ()
	shell := os.Getenv("SHELL")
	if shell == "" {
		return Snapshot{Env: base, Source: "DHI's own environment", Err: errors.New("SHELL is not set")}
	}
	start := time.Now()
	env, err := probe(shell)
	took := time.Since(start)
	if err != nil {
		return Snapshot{Env: base, Shell: shell, Took: took, Source: "DHI's own environment", Err: err}
	}
	return Snapshot{Env: merge(env, base), Shell: shell, Took: took, Source: "login shell"}
}

// merge keeps what the login shell set, plus DHI's own variables the
// shell does not know (TERM and friends from the terminal DHI runs in).
func merge(shell, own []string) []string {
	have := map[string]bool{}
	for _, kv := range shell {
		if k, _, ok := strings.Cut(kv, "="); ok {
			have[k] = true
		}
	}
	out := append([]string(nil), shell...)
	for _, kv := range own {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || have[k] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func probeShell(shell string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	// -i loads interactive rc files (where nvm and friends live), -l the
	// login profile. The marker separates their noise from the dump.
	cmd := exec.CommandContext(ctx, shell, "-ilc", "printf '"+marker+"'; env -0")
	cmd.Env = append(os.Environ(), "DHI_RESOLVING_ENV=1")
	cmd.Stdin = nil
	// A new session with no controlling terminal: an interactive shell
	// that reaches for the tty would otherwise stop and hang the probe.
	detach(cmd)
	// On timeout kill the whole group, and stop waiting for output that
	// something the rc files started in the background still holds.
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("the login shell took more than %s", Timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("the login shell failed: %w", err)
	}
	return Parse(out)
}

// Parse reads `env -0` output after the marker.
func Parse(out []byte) ([]string, error) {
	i := bytes.LastIndex(out, []byte(marker))
	if i < 0 {
		return nil, errors.New("no environment in the shell's output")
	}
	var env []string
	for _, rec := range bytes.Split(out[i+len(marker):], []byte{0}) {
		kv := string(bytes.TrimLeft(rec, "\n"))
		if k, _, ok := strings.Cut(kv, "="); ok && k != "" && !strings.ContainsAny(k, " \n") {
			if k == "DHI_RESOLVING_ENV" || k == "_" || k == "SHLVL" || k == "PWD" || k == "OLDPWD" {
				continue
			}
			env = append(env, kv)
		}
	}
	if len(env) == 0 {
		return nil, errors.New("the shell printed an empty environment")
	}
	return env, nil
}

// Apply makes the resolved environment DHI's own (os.Setenv), so every
// os.Environ and exec.LookPath in the process sees what the user's shell
// sees — the way VS Code applies the resolved shell environment to its
// process.
func Apply() Snapshot {
	s := Load()
	for _, kv := range s.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			_ = os.Setenv(k, v)
		}
	}
	return s
}
