package install

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/drjzlyan/dhi/internal/agentkit/hooks"
)

// HookRecord is one plugin's stored hooks (F-179, ADR-0047). The plugin
// folder under Scope.HooksDir is what runs; Digest is what the user saw
// when installing, so a changed folder is refused rather than run.
type HookRecord struct {
	Source  string `toml:"source"`
	Commit  string `toml:"commit,omitempty"`
	Digest  string `toml:"digest,omitempty"`
	Enabled bool   `toml:"enabled,omitempty"` // off until the user turns it on
}

// HookInfo is one stored plugin for the Settings list.
type HookInfo struct {
	Plugin  string
	Enabled bool
	Set     hooks.Set
	Problem string // why it cannot run now ("" = fine)
}

// InstalledHooks lists the plugins with stored hooks in scope.
func InstalledHooks(scope Scope) ([]HookInfo, error) {
	if scope.LockPath == "" {
		return nil, nil
	}
	lock, err := ReadLock(scope.LockPath)
	if err != nil {
		return nil, err
	}
	var out []HookInfo
	for name, rec := range lock.Hooks {
		hi := HookInfo{Plugin: name, Enabled: rec.Enabled}
		p, problem := loadHooks(scope, name, rec)
		hi.Problem = problem
		if p != nil {
			hi.Set = p.Set
		}
		out = append(out, hi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Plugin < out[j].Plugin })
	return out, nil
}

// SetHooksEnabled turns a plugin's hooks on or off. Turning on checks the
// stored folder still matches the digest the user was shown.
func SetHooksEnabled(scope Scope, name string, on bool) error {
	lock, err := ReadLock(scope.LockPath)
	if err != nil {
		return err
	}
	rec, ok := lock.Hooks[name]
	if !ok {
		return fmt.Errorf("no hooks stored for plugin %q in %s", name, scope.Name)
	}
	if on {
		if _, problem := loadHooks(scope, name, rec); problem != "" {
			return fmt.Errorf("%s", problem)
		}
	}
	rec.Enabled = on
	lock.Hooks[name] = rec
	return lock.Write(scope.LockPath)
}

// EnabledHooks returns the plugins in scope whose hooks are on, and a named
// problem for each that cannot run (changed since install, unreadable).
func EnabledHooks(scope Scope) ([]hooks.Plugin, []string) {
	if scope.LockPath == "" {
		return nil, nil
	}
	lock, err := ReadLock(scope.LockPath)
	if err != nil {
		return nil, []string{fmt.Sprintf("hooks: %v", err)}
	}
	names := make([]string, 0, len(lock.Hooks))
	for n, rec := range lock.Hooks {
		if rec.Enabled {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out []hooks.Plugin
	var problems []string
	for _, n := range names {
		p, problem := loadHooks(scope, n, lock.Hooks[n])
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		out = append(out, *p)
	}
	return out, problems
}

// loadHooks reads a stored plugin and checks it against its digest.
func loadHooks(scope Scope, name string, rec HookRecord) (*hooks.Plugin, string) {
	dir := filepath.Join(scope.HooksDir, name)
	if rec.Digest != "" {
		dg, err := Digest(dir)
		if err != nil {
			return nil, fmt.Sprintf("hooks of %s: cannot read %s: %v", name, dir, err)
		}
		if dg != rec.Digest {
			return nil, fmt.Sprintf("hooks of %s: the stored copy changed since install; reinstall the plugin to review its hooks again", name)
		}
	}
	pl, err := ReadPlugin(dir)
	if err != nil {
		return nil, fmt.Sprintf("hooks of %s: %v", name, err)
	}
	return &hooks.Plugin{Name: name, Dir: dir, Set: pl.Hooks}, ""
}
