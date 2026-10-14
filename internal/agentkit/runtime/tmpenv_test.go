package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// F-086: a sandboxed CLI cannot write the host's /tmp or /var/folders,
// so every spawn gets a private TMPDIR inside the workspace — and claude
// its CLAUDE_CODE_TMPDIR too, which it reads instead of TMPDIR for the
// per-uid dir its Bash tool needs before running anything.
func TestCLIEnvPointsTempAtThePrivateAgentDir(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.CLIEnv = []string{"PATH=/bin", "TMPDIR=/var/folders/host/T/"}
	env := h.rt.cliEnv(clirun.Claude, "anvil", "")
	want := filepath.Join(h.ws.Root, workspace.DirTmp, "anvil")
	got := map[string][]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = append(got[k], v)
	}
	if got["MCP_TOOL_TIMEOUT"][0] != "3600000" {
		t.Fatalf("MCP_TOOL_TIMEOUT = %v", got["MCP_TOOL_TIMEOUT"])
	}
	for _, k := range []string{"TMPDIR", "CLAUDE_CODE_TMPDIR"} {
		if len(got[k]) != 1 || got[k][0] != want {
			t.Fatalf("%s = %v, want exactly [%s]", k, got[k], want)
		}
	}
	fi, err := os.Stat(want)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("private tmp dir: %v %v", fi, err)
	}
}

func TestCleanTmpDropsOnlyStaleEntries(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, workspace.DirTmp, "anvil")
	if err := os.MkdirAll(filepath.Join(dir, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old", "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "old", "f"), long, long)
	_ = os.Chtimes(filepath.Join(dir, "old"), long, long)
	if n := CleanTmp(root, 24*time.Hour, time.Now()); n != 1 {
		t.Fatalf("removed %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "fresh")); err != nil {
		t.Fatal("a fresh entry was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Fatal("the stale entry stayed")
	}
}

func TestCLIEnvCarriesThePushGuard(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.PushGuard = "/ws/.dhi/hooks/dhi-pre-push"
	env := strings.Join(h.rt.cliEnv(clirun.Claude, "anvil", ""), "\n")
	if !strings.Contains(env, "=hook.dhi-default-branch.event\n") || !strings.Contains(env, "=/ws/.dhi/hooks/dhi-pre-push") {
		t.Fatalf("env lacks the push guard:\n%s", env)
	}
}

// Live (M32): cursor-agent is "#!/usr/bin/env bash" and env found
// Homebrew's bash; its folder must join the exec roots.
func TestInterpreterDirsResolvesEnvOnThePath(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "bash"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "cli")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := interpreterDirs(script, bin+string(os.PathListSeparator)+"/bin")
	if len(got) == 0 || got[0] != bin {
		t.Fatalf("dirs = %v, want %s first", got, bin)
	}
	native := filepath.Join(t.TempDir(), "native")
	_ = os.WriteFile(native, []byte{0xcf, 0xfa, 0xed, 0xfe}, 0o755)
	if interpreterDirs(native, bin) != nil {
		t.Fatal("a binary has no interpreter")
	}
}

// Live (M32): in a linked worktree the exclude must reach the common
// dir, the only info/exclude git reads.
func TestExcludeFromGitInALinkedWorktree(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "repo", ".git")
	wtGit := filepath.Join(common, "worktrees", "wt")
	if err := os.MkdirAll(wtGit, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o644)
	wt := filepath.Join(root, "wt")
	_ = os.MkdirAll(wt, 0o755)
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o644)
	excludeFromGit(wt, ".cursor/mcp.json")
	b, err := os.ReadFile(filepath.Join(common, "info", "exclude"))
	if err != nil || !strings.Contains(string(b), "/.cursor/mcp.json") {
		t.Fatalf("common exclude = %q %v", b, err)
	}
}
