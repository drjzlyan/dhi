package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
