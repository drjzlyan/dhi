// Package boot resolves what launch must do before any surface renders
// (F-011 / ADR-0011, amended by ADR-0044): proceed, or block with named
// reasons and fixes. All resolution policy lives here and is
// table-tested; cmd/dhi only renders the decision. Tools never block
// boot: they come from the user's system, and a missing one raises its
// install card where it is needed (F-110).
package boot

import (
	"os"
	"path/filepath"

	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/workspace"
	"github.com/drjzlyan/dhi/internal/worktrees"
)

// Input carries everything the audit needs; injectable fields make the
// decision table-testable without a real host.
type Input struct {
	CWD      string // launch directory (workspace probe)
	UserCfg  string // settings user layer ("" ok)
	Home     string // home directory for `~` in worktrees.root ("" = the process's)
	GOOS     string
	LookPath func(string) (string, error)
	// ReadOnly are extra trees sandboxed children may read and execute
	// but never write: your skills for every workspace (F-091), whose
	// scripts skill_run starts. Missing dirs are skipped.
	ReadOnly []string
}

// Decision is the resolution outcome.
type Decision struct {
	// Block non-empty: boot refuses with this reason; Fixes say how to
	// clear it. Nothing else in the decision is meaningful then.
	Block string
	Fixes []string

	// Sandbox is the adapter for runtime.Config (nil when no workspace
	// is loaded — the runtime is never built then — or boot blocked).
	Sandbox sandbox.Sandbox

	// Worktrees says where linked worktrees live (F-053). The zero value is
	// the in-workspace default; an external base has been created and is
	// inside the OS sandbox's writable roots.
	Worktrees worktrees.Layout

	// Warnings label explicit opt-outs that survive boot (e.g. the user
	// set security.sandbox = "off"). Never silent, never implied.
	Warnings []string
}

// Audit resolves the full launch decision in a fixed order: workspace →
// settings → sandbox. The first hard failure blocks; every
// block names the offending file or seam and how to fix it.
func Audit(in Input) Decision {
	d := Decision{}

	// 1. Workspace: a plain directory boots into the empty-state
	// editor; a workspace with a broken config refuses with the reason.
	wsCfg := ""
	var members []string
	ws, wsErr := workspace.Load(in.CWD)
	switch {
	case workspace.IsConfigError(wsErr):
		d.Block = wsErr.Error()
		d.Fixes = []string{"repair " + filepath.Join(in.CWD, workspace.ConfigFile) + " (dhi doctor shows details)"}
		return d
	case wsErr == nil:
		for _, m := range ws.Members() {
			members = append(members, m.Path)
		}
		wsCfg = filepath.Join(ws.Root, workspace.DHIDir, "config.toml")
	}

	// 2. Settings, strict: malformed/unknown/out-of-range refuse boot
	// naming file + key (no silent substitution).
	cfg, cfgErr := settings.Load(in.UserCfg, wsCfg)
	if cfgErr != nil {
		d.Block = cfgErr.Error()
		d.Fixes = []string{"fix or remove the named keys, or delete the config file to reset to defaults"}
		return d
	}

	// 3. Sandbox: hard requirement in auto mode; explicit off is the
	// user's choice and is labeled, never implied.
	rw := make([]string, 0, len(members)+3)
	rw = append(rw, members...)
	if ws != nil {
		rw = append(rw, filepath.Join(ws.Root, workspace.DHIDir))
		// A user-chosen worktree location must be writable by the sandbox,
		// so it is resolved and created here, before the profile is built.
		home := in.Home
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		lay, err := worktrees.Resolve(ws.Root, cfg.Worktrees.Root, home)
		if err == nil {
			err = lay.Ensure()
		}
		if err != nil {
			d.Block = err.Error()
			d.Fixes = []string{"fix worktrees.root in " + filepath.Join(ws.Root, workspace.DHIDir, "config.toml") +
				" or your user settings, or remove it to keep worktrees inside .dhi/"}
			return d
		}
		d.Worktrees = lay
		if lay.External() {
			base := lay.Base()
			if resolved, err := filepath.EvalSymlinks(base); err == nil {
				base = resolved // seatbelt matches canonical paths (/var → /private/var)
			}
			rw = append(rw, base)
		}
	}
	var ro []string
	for _, r := range in.ReadOnly {
		if resolved, err := filepath.EvalSymlinks(r); err == nil {
			ro = append(ro, resolved)
		}
	}
	switch cfg.Security.Sandbox {
	case settings.SandboxOff:
		d.Sandbox = sandbox.Noop{}
		d.Warnings = append(d.Warnings,
			"os sandbox disabled by config (explicit opt-out; path-jail + policy still apply)")
	default:
		if len(rw) == 0 {
			// Nothing to confine yet (no workspace, no prefix) — the
			// strict requirement is still on the helper's presence.
			if err := sandbox.Require(in.GOOS, in.LookPath); err != nil {
				d.Block = err.Error()
				d.Fixes = []string{
					"install the OS sandbox helper (sandbox-exec on macOS is built in; bwrap on Linux)",
					"or set security.sandbox = \"off\" in settings to opt out explicitly",
				}
				return d
			}
			break
		}
		sb, err := sandbox.Select(in.GOOS, in.LookPath, cfg.Security.Sandbox, rw, ro)
		if err != nil {
			d.Block = err.Error()
			d.Fixes = []string{
				"install the OS sandbox helper (sandbox-exec on macOS is built in; bwrap on Linux)",
				"or set security.sandbox = \"off\" in settings to opt out explicitly",
			}
			return d
		}
		d.Sandbox = sb
	}

	return d
}

// SandboxMode re-reads the sandbox setting for surfaces that need to
// label it after boot; broken configs read as auto (doctor reports the
// breakage separately).
func SandboxMode(userCfg, wsCfg string) string {
	cfg, err := settings.LoadBestEffort(userCfg, wsCfg)
	if err != nil {
		return settings.SandboxAuto
	}
	return cfg.Security.Sandbox
}
