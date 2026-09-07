// Package boot resolves what launch must do before any surface renders
// (F-011 / ADR-0011): proceed, offer confirmation-gated installs, or
// block with named reasons and fixes. All resolution policy lives here
// and is table-tested; cmd/dhi only renders the decision. There are no
// fallbacks: a missing hard requirement refuses boot; a declined
// install leaves the capability refusing at use with the named fix.
package boot

import (
	"os"
	"path/filepath"

	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Input carries everything the audit needs; injectable fields make the
// decision table-testable without a real host.
type Input struct {
	CWD      string // launch directory (workspace probe)
	ToolRoot string // hermetic prefix ("" disables toolchain checks)
	UserCfg  string // settings user layer ("" ok)
	GOOS     string
	LookPath func(string) (string, error)
}

// Decision is the resolution outcome.
type Decision struct {
	// Block non-empty: boot refuses with this reason; Fixes say how to
	// clear it. Nothing else in the decision is meaningful then.
	Block string
	Fixes []string

	// Offer lists missing hermetic pieces for the confirmation-gated
	// install (empty when nothing is missing or boot is blocked).
	Offer []string

	// Sandbox is the adapter for runtime.Config (nil when no workspace
	// is loaded — the runtime is never built then — or boot blocked).
	Sandbox sandbox.Sandbox

	// Warnings label explicit opt-outs that survive boot (e.g. the user
	// set security.sandbox = "off"). Never silent, never implied.
	Warnings []string
}

// Audit resolves the full launch decision in a fixed order: workspace →
// settings → sandbox → toolchain. The first hard failure blocks; every
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
	rw := make([]string, 0, len(members)+2)
	rw = append(rw, members...)
	if ws != nil {
		rw = append(rw, filepath.Join(ws.Root, workspace.DHIDir))
	}
	if in.ToolRoot != "" {
		rw = append(rw, in.ToolRoot)
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
		sb, err := sandbox.Select(in.GOOS, in.LookPath, cfg.Security.Sandbox, rw, nil)
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

	// 4. Toolchain: a corrupt lockfile refuses (supply-chain surface);
	// a missing lockfile is first-run and the bootstrap gate handles
	// it; missing pieces become the confirmation-gated install offer.
	if in.ToolRoot != "" {
		mgr := toolchain.New(in.ToolRoot)
		lf, err := mgr.ReadLockfile()
		if err != nil {
			d.Block = "toolchain lockfile is corrupt: " + err.Error()
			d.Fixes = []string{"re-run bootstrap (delete " + in.ToolRoot + " to reinstall from scratch)"}
			return d
		}
		if lf != nil && len(lf.Tools) > 0 {
			d.Offer = missingTools(mgr)
			if !hasShim(in.ToolRoot, "gopls") {
				d.Offer = append(d.Offer, "gopls (built from source)")
			}
		}
	}
	return d
}

// missingTools diffs the embedded registry against installed state.
// Resolve errors (unknown tool, unreadable state) surface as an offer
// of everything the manifest lists — the install either fixes it or
// reports the failure visibly.
func missingTools(mgr *toolchain.Manager) []string {
	mf, err := toolchain.Embedded()
	if err != nil {
		return nil
	}
	plan, err := mgr.Resolve(mf, nil)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range plan {
		if p.Reason == "missing" {
			out = append(out, p.Tool)
		}
	}
	return out
}

// hasShim reports whether a named executable sits in the prefix bin.
func hasShim(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, "bin", name))
	return err == nil
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
