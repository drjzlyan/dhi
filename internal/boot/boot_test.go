package boot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/toolchain"
)

func lookHit(string) (string, error) { return "/bin/helper", nil }
func lookMiss(string) (string, error) {
	return "", errors.New("exec: not found")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkWorkspace(t *testing.T, cfg string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".dhi", "workspace.toml"), cfg)
	// member dir must exist for Load to accept the roster
	if strings.Contains(cfg, "repos/alpha") {
		writeFile(t, filepath.Join(root, "repos", "alpha", "keep.txt"), "x")
	}
	return root
}

const wsCfg = "schema = 1\n\n[members.alpha]\npath = \"repos/alpha\"\n"

func TestPlainDirectoryBootsEmptyState(t *testing.T) {
	d := Audit(Input{CWD: t.TempDir(), GOOS: "darwin", LookPath: lookHit})
	if d.Block != "" {
		t.Fatalf("plain dir must boot empty-state, blocked: %s", d.Block)
	}
	// no workspace → nothing to confine → no adapter yet, but the
	// helper presence was still required (lookHit passed).
	if d.Sandbox != nil {
		t.Fatalf("no-workspace boot carries no adapter, got %v", d.Sandbox)
	}
	if len(d.Offer) != 0 {
		t.Fatalf("unexpected offer: %v", d.Offer)
	}
}

func TestMissingSandboxHelperBlocksBoot(t *testing.T) {
	d := Audit(Input{CWD: t.TempDir(), GOOS: "darwin", LookPath: lookMiss})
	if d.Block == "" || !strings.Contains(d.Block, "sandbox-exec") {
		t.Fatalf("block = %q", d.Block)
	}
	if len(d.Fixes) != 2 {
		t.Fatalf("fixes = %v", d.Fixes)
	}
	if d.Sandbox != nil {
		t.Fatal("blocked decision must not carry an adapter")
	}
}

func TestSandboxOffIsExplicitOptOutNotFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	writeFile(t, p, "[security]\nsandbox = \"off\"\n")
	d := Audit(Input{CWD: t.TempDir(), UserCfg: p, GOOS: "darwin", LookPath: lookMiss})
	if d.Block != "" {
		t.Fatalf("explicit off must boot: %s", d.Block)
	}
	if d.Sandbox == nil || d.Sandbox.Name() != "noop" {
		t.Fatalf("sandbox = %v", d.Sandbox)
	}
	if len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "explicit opt-out") {
		t.Fatalf("warnings = %v", d.Warnings)
	}
}

func TestUnsupportedPlatformBlocks(t *testing.T) {
	d := Audit(Input{CWD: t.TempDir(), GOOS: "plan9", LookPath: lookHit})
	if d.Block == "" || !strings.Contains(d.Block, "plan9") {
		t.Fatalf("block = %q", d.Block)
	}
}

func TestBrokenWorkspaceConfigBlocks(t *testing.T) {
	root := mkWorkspace(t, "schema = 99\n")
	d := Audit(Input{CWD: root, GOOS: "darwin", LookPath: lookHit})
	if d.Block == "" || !strings.Contains(d.Block, "schema") {
		t.Fatalf("block = %q", d.Block)
	}
	if len(d.Fixes) != 1 || !strings.Contains(d.Fixes[0], "workspace.toml") {
		t.Fatalf("fixes = %v", d.Fixes)
	}
}

func TestBrokenSettingsBlockBoot(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	writeFile(t, p, "[security]\nsandbox = \"yolo\"\n")
	d := Audit(Input{CWD: t.TempDir(), UserCfg: p, GOOS: "darwin", LookPath: lookHit})
	if d.Block == "" || !strings.Contains(d.Block, `security.sandbox "yolo"`) {
		t.Fatalf("block = %q", d.Block)
	}
	if len(d.Fixes) != 1 {
		t.Fatalf("fixes = %v", d.Fixes)
	}
}

func TestCorruptLockfileBlocks(t *testing.T) {
	root := mkWorkspace(t, wsCfg)
	toolRoot := t.TempDir()
	writeFile(t, filepath.Join(toolRoot, "lock.json"), "{broken")
	d := Audit(Input{CWD: root, ToolRoot: toolRoot, GOOS: "darwin", LookPath: lookHit})
	if d.Block == "" || !strings.Contains(d.Block, "lockfile") {
		t.Fatalf("block = %q", d.Block)
	}
	if len(d.Fixes) != 1 || !strings.Contains(d.Fixes[0], "bootstrap") {
		t.Fatalf("fixes = %v", d.Fixes)
	}
}

func TestMissingLockfileMeansFirstRun(t *testing.T) {
	root := mkWorkspace(t, wsCfg)
	d := Audit(Input{CWD: root, ToolRoot: t.TempDir(), GOOS: "darwin", LookPath: lookHit})
	if d.Block != "" {
		t.Fatalf("first run must not block: %s", d.Block)
	}
	// first-run install is the bootstrap gate's job, not the offer list
	if len(d.Offer) != 0 {
		t.Fatalf("offer = %v", d.Offer)
	}
}

func TestPartialInstallOffersMissing(t *testing.T) {
	root := mkWorkspace(t, wsCfg)
	toolRoot := t.TempDir()
	// go is installed at the manifest version; rg/uv/node/git are not.
	writeFile(t, filepath.Join(toolRoot, "lock.json"),
		`{"schema":1,"updated_at":"2026-09-02T00:00:00Z","tools":{"go":{"version":"1.27.0","sha256":"x","path":"tools/go"}}}`)
	d := Audit(Input{CWD: root, ToolRoot: toolRoot, GOOS: "darwin", LookPath: lookHit})
	if d.Block != "" {
		t.Fatalf("partial install must not block: %s", d.Block)
	}
	joined := strings.Join(d.Offer, ",")
	for _, want := range []string{"rg", "uv", "node", "git", "gh", "gopls", "dlv (built from source)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("offer missing %q: %v", want, d.Offer)
		}
	}
	if strings.Contains(joined, "go,") || strings.HasSuffix(joined, "go") && !strings.Contains(joined, "gopls") {
		t.Errorf("installed go offered: %v", d.Offer)
	}
}

func TestFullInstallOffersNothing(t *testing.T) {
	root := mkWorkspace(t, wsCfg)
	toolRoot := t.TempDir()
	writeFile(t, filepath.Join(toolRoot, "lock.json"),
		`{"schema":1,"updated_at":"2026-09-02T00:00:00Z","tools":{"go":{"version":"1.27.0","sha256":"x","path":"t"},"rg":{"version":"15.2.0","sha256":"x","path":"t"},"uv":{"version":"0.12.5","sha256":"x","path":"t"},"node":{"version":"24.19.0","sha256":"x","path":"t"},"git":{"version":"2.55.0","sha256":"x","path":"t"},"gh":{"version":"2.100.0","sha256":"x","path":"t"}}}`)
	writeFile(t, filepath.Join(toolRoot, "bin", "gopls"), "x")
	writeFile(t, filepath.Join(toolRoot, "bin", "dlv"), "x")
	d := Audit(Input{CWD: root, ToolRoot: toolRoot, GOOS: "darwin", LookPath: lookHit})
	if d.Block != "" {
		t.Fatalf("full install must not block: %s", d.Block)
	}
	if len(d.Offer) != 0 {
		t.Fatalf("offer = %v", d.Offer)
	}
}

func TestSandboxRootsIncludeWorkspaceAndPrefix(t *testing.T) {
	// seatbelt Wrap must carry the workspace root + .dhi + tool prefix.
	root := mkWorkspace(t, wsCfg)
	toolRoot := t.TempDir()
	writeFile(t, filepath.Join(toolRoot, "lock.json"), `{"schema":1,"tools":{}}`)
	d := Audit(Input{CWD: root, ToolRoot: toolRoot, GOOS: "darwin", LookPath: lookHit})
	if d.Block != "" || d.Sandbox == nil {
		t.Fatalf("decision = %+v", d)
	}
	seat, ok := d.Sandbox.(interface{ Profile() string })
	if !ok {
		t.Fatal("sandbox is not the seatbelt adapter")
	}
	p := seat.Profile()
	for _, want := range []string{filepath.Join(root, "repos", "alpha"), filepath.Join(root, ".dhi"), toolRoot} {
		if !strings.Contains(p, want) {
			t.Errorf("profile missing root %s:\n%s", want, p)
		}
	}
}

func TestSandboxModeHelper(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	writeFile(t, p, "[security]\nsandbox = \"off\"\n")
	if got := SandboxMode(p, ""); got != settings.SandboxOff {
		t.Fatalf("mode = %q", got)
	}
	// broken config reads as auto here; doctor reports the breakage.
	writeFile(t, p, "garbage!!!")
	if got := SandboxMode(p, ""); got != settings.SandboxAuto {
		t.Fatalf("broken config mode = %q", got)
	}
}

func TestDefaultWorktreeLayoutStaysInsideTheWorkspace(t *testing.T) {
	root := mkWorkspace(t, wsCfg)
	d := Audit(Input{CWD: root, GOOS: "darwin", LookPath: lookHit})
	if d.Block != "" {
		t.Fatal(d.Block)
	}
	if d.Worktrees.External() {
		t.Fatalf("no setting must keep the defaults: %q", d.Worktrees.Base())
	}
}

// A worktree root outside the workspace is created at boot and made
// writable for the OS sandbox - otherwise agents could not work in it.
func TestExternalWorktreeRootIsCreatedAndWritableInTheSandbox(t *testing.T) {
	root := mkWorkspace(t, wsCfg)
	wt := filepath.Join(t.TempDir(), "fast-disk")
	cfg := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, cfg, "[worktrees]\nroot = \""+wt+"\"\n")
	d := Audit(Input{CWD: root, UserCfg: cfg, GOOS: "darwin", LookPath: lookHit})
	if d.Block != "" {
		t.Fatalf("block = %q", d.Block)
	}
	if !d.Worktrees.External() || !strings.HasPrefix(d.Worktrees.Base(), wt) {
		t.Fatalf("layout = %q, want it under %q", d.Worktrees.Base(), wt)
	}
	if st, err := os.Stat(d.Worktrees.Base()); err != nil || !st.IsDir() {
		t.Fatalf("base was not created: %v", err)
	}
	argv, err := d.Sandbox.Wrap([]string{"/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := filepath.EvalSymlinks(d.Worktrees.Base())
	if profile := strings.Join(argv, " "); !strings.Contains(profile, canonical) {
		t.Fatalf("the sandbox profile does not allow writes under %s:\n%s", canonical, profile)
	}
}

func TestUnusableWorktreeRootBlocksBootWithTheFix(t *testing.T) {
	root := mkWorkspace(t, wsCfg)
	blocker := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, blocker, "not a directory")
	cfg := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, cfg, "[worktrees]\nroot = \""+filepath.Join(blocker, "wt")+"\"\n")
	d := Audit(Input{CWD: root, UserCfg: cfg, GOOS: "darwin", LookPath: lookHit})
	if d.Block == "" || !strings.Contains(d.Block, "worktrees") {
		t.Fatalf("block = %q", d.Block)
	}
	if len(d.Fixes) != 1 || !strings.Contains(d.Fixes[0], "worktrees.root") {
		t.Fatalf("fixes = %v", d.Fixes)
	}
	if d.Sandbox != nil {
		t.Fatal("a blocked decision must not carry an adapter")
	}

	// "~" with no known home is refused by name, not guessed.
	writeFile(t, cfg, "[worktrees]\nroot = \"~/wt\"\n")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	d = Audit(Input{CWD: root, UserCfg: cfg, GOOS: "darwin", LookPath: lookHit})
	if d.Block == "" || !strings.Contains(d.Block, "home") {
		t.Fatalf("block = %q", d.Block)
	}
}

// TestToolchainIsReadOnlyToAgents pins the ro-root differentiation: the
// pinned toolchain is readable/executable but not writable inside the
// sandbox; only its runtime caches are.
func TestToolchainIsReadOnlyToAgents(t *testing.T) {
	toolRoot := t.TempDir()
	d := Audit(Input{CWD: t.TempDir(), ToolRoot: toolRoot, GOOS: "darwin", LookPath: lookHit})
	sb, ok := d.Sandbox.(*sandbox.Seatbelt)
	if !ok {
		t.Fatalf("sandbox = %T (block %q)", d.Sandbox, d.Block)
	}
	var write string
	for _, line := range strings.Split(sb.Profile(), "\n") {
		if strings.HasPrefix(line, "(allow file-write*") {
			write = line
		}
	}
	if strings.Contains(write, `"`+toolRoot+`"`) {
		t.Fatalf("the toolchain root is writable:\n%s", write)
	}
	for _, sub := range toolchain.WritableSubdirs {
		if !strings.Contains(write, filepath.Join(toolRoot, sub)) {
			t.Errorf("%s must stay writable:\n%s", sub, write)
		}
	}
	if !strings.Contains(sb.Profile(), `(subpath "`+toolRoot+`")`) {
		t.Fatal("the toolchain must stay readable/executable")
	}
}
