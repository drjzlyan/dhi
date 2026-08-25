package toolchain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildSpec describes a tool built from source with DHI's own pinned Go
// toolchain — the path for servers like gopls that publish no official
// binaries (golang/go#79066). The build runs fully sandboxed to the
// prefix: GOTOOLCHAIN=local, GOPATH/GOMODCACHE/GOCACHE under a staging
// dir that is removed afterwards.
type BuildSpec struct {
	Name    string // tool name ("gopls")
	Version string // module version tag ("v0.23.0")
	Module  string // module path ("golang.org/x/tools/gopls")
	BinName string // produced binary ("gopls"); defaults to Name
	Shims   []string
	// LocalDir builds an existing module directory instead of fetching
	// Module@Version (tests, vendored tools).
	LocalDir string
}

// Gopls is DHI's known source-built server.
func Gopls() BuildSpec {
	return BuildSpec{
		Name:    "gopls",
		Version: "v0.23.0",
		Module:  "golang.org/x/tools/gopls",
		Shims:   []string{"gopls"},
	}
}

// BuildInstall compiles spec with the installed go shim and activates
// it like any other tool: tools/<name>/<version>/bin/<bin>, shims
// linked, lockfile updated. Requires the "go" tool to be installed.
func (m *Manager) BuildInstall(ctx context.Context, spec BuildSpec) error {
	goBin, err := m.resolveGoBinary()
	if err != nil {
		return err
	}
	binName := spec.BinName
	if binName == "" {
		binName = spec.Name
	}
	shims := spec.Shims
	if len(shims) == 0 {
		shims = []string{spec.Name}
	}

	m.emit(Event{Kind: EventDownloadStart, Tool: spec.Name})

	stagingBase := filepath.Join(m.root, "staging")
	if err := os.MkdirAll(stagingBase, 0o755); err != nil {
		return fmt.Errorf("toolchain: staging: %w", err)
	}
	stageDir, err := os.MkdirTemp(stagingBase, spec.Name+"-build-")
	if err != nil {
		return fmt.Errorf("toolchain: staging: %w", err)
	}
	// Closure so writable() runs at teardown, not at defer-registration.
	defer func() { _ = os.RemoveAll(writable(stageDir)) }() // module caches are read-only

	gopath := filepath.Join(stageDir, "gopath")
	gobin := filepath.Join(stageDir, "out")
	for _, d := range []string{gopath, gobin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("toolchain: staging: %w", err)
		}
	}

	env := append([]string{},
		"GOTOOLCHAIN=local", // never let the build pull another toolchain
		"GOBIN="+gobin,
		"GOPATH="+gopath,
		"CGO_ENABLED=0",
	)
	env = append(env, m.Env(nil)...)

	argv := []string{"install", spec.Module + "@" + spec.Version}
	cmdDir := stageDir
	if spec.LocalDir != "" {
		argv = []string{"build", "-o", gobin, "./"}
		cmdDir = spec.LocalDir
	}
	cmd := exec.CommandContext(ctx, goBin, argv...)
	cmd.Dir = cmdDir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("toolchain: build %s: %v: %s", spec.Name, err, tailRunes(out, 400))
	}

	srcBin := filepath.Join(gobin, binName)
	if spec.LocalDir != "" {
		// go build -o dir produces <dir>/<module base>
		if _, err := os.Stat(srcBin); err != nil {
			srcBin = filepath.Join(gobin, filepath.Base(spec.LocalDir))
		}
	}
	if _, err := os.Stat(srcBin); err != nil {
		return fmt.Errorf("toolchain: build %s: binary not produced: %w", spec.Name, err)
	}

	target := m.toolDir(spec.Name, strings.TrimPrefix(spec.Version, "v"))
	_ = os.RemoveAll(target)
	binDir := filepath.Join(target, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("toolchain: activate: %w", err)
	}
	if err := os.Rename(srcBin, filepath.Join(binDir, binName)); err != nil {
		return fmt.Errorf("toolchain: activate: %w", err)
	}

	digest, err := HashFile(filepath.Join(binDir, binName))
	if err != nil {
		return err
	}
	lf, err := m.ReadLockfile()
	if err != nil {
		return err
	}
	lf.Tools[spec.Name] = LockedTool{
		Version: strings.TrimPrefix(spec.Version, "v"),
		SHA256:  digest,
		Path:    filepath.Join("tools", spec.Name, strings.TrimPrefix(spec.Version, "v")),
	}
	if err := m.writeLockfile(lf); err != nil {
		return err
	}
	if err := m.linkShims(spec.Name, Tool{Shims: shims}, target, "bin"); err != nil {
		return err
	}

	m.emit(Event{Kind: EventVerified, Tool: spec.Name})
	m.emit(Event{Kind: EventToolDone, Tool: spec.Name})
	return nil
}

func tailRunes(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// writable chmods dir contents so RemoveAll can delete go module
// caches (which mark files 0444 and dirs 0555).
func writable(dir string) string {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			_ = os.Chmod(path, 0o700)
		} else {
			_ = os.Chmod(path, 0o600)
		}
		return nil
	})
	return dir
}

// resolveGoBinary maps the go shim to its real binary. Building must
// bypass the shim itself: the shim dir is first on the build PATH (via
// Env), and `exec go` inside the shim would otherwise recurse into
// itself forever.
func (m *Manager) resolveGoBinary() (string, error) {
	shim := filepath.Join(m.ShimDir(), "go")
	real, err := filepath.EvalSymlinks(shim)
	if err != nil {
		return "", fmt.Errorf("toolchain: build requires the go tool (install it first): %w", err)
	}
	return real, nil
}
