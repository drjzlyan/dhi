package runtime

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// TestLiveClaudeSeatbeltSmoke is the DHI_SMOKE_CLAUDE=1 verification that
// a REAL claude CLI runs through the REAL seatbelt wrap with the fixed
// binary-first argv contract: stream-json comes back, Finalize succeeds.
// Skipped unless DHI_SMOKE_CLAUDE=1 (needs claude on PATH + auth + network).
func TestLiveClaudeSeatbeltSmoke(t *testing.T) {
	if os.Getenv("DHI_SMOKE_CLAUDE") == "" {
		t.Skip("set DHI_SMOKE_CLAUDE=1 to run the live claude+seatbelt smoke")
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	rw := []string{root, filepath.Join(home, ".claude"), "/tmp"}
	sb, err := sandbox.Select("darwin", exec.LookPath, "auto", rw, nil)
	if err != nil {
		t.Fatalf("sandbox select: %v", err)
	}
	t.Logf("sandbox: %s", sb.Name())

	reg := clirun.NewRegistry(exec.LookPath)
	claudePath, err := reg.Path("claude")
	if err != nil {
		t.Fatalf("claude: %v", err)
	}
	// Mirror the runtime's extension: admit the CLI's state + binary
	// roots (claude lives under ~/.local, not the workspace jail).
	if re, ok := sb.(sandbox.RootExtender); ok {
		resolved, rerr := filepath.EvalSymlinks(claudePath)
		if rerr != nil {
			t.Fatal(rerr)
		}
		sb, err = re.WithExtraRoots([]string{
			filepath.Dir(claudePath),
			filepath.Dir(resolved),
			filepath.Dir(filepath.Dir(resolved)),
		})
		if err != nil {
			t.Fatalf("extend: %v", err)
		}
	}

	argv := append([]string{claudePath}, clirunBuild(t, reg)(clirun.RunInput{
		Prompt: "Reply with exactly: PONG",
	})...)
	wrapped, err := sb.Wrap(argv)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	t.Logf("wrapped[0]=%s argv=%v", wrapped[0], wrapped[:min(4, len(wrapped))])

	cmd := exec.CommandContext(context.Background(), wrapped[0], wrapped[1:]...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "ANTHROPIC_API_KEY="+os.Getenv("ANTHROPIC_API_KEY"))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	var final string
	for ev := range clirunParse(t, reg)(out) {
		t.Logf("event %s: %s", ev.Kind, truncateRunes(ev.Detail, 90))
		if ev.Kind == clirun.EventFinal {
			final = ev.Detail
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		t.Fatalf("wait: %v", waitErr)
	}
	if final == "" {
		t.Fatal("no final result line — the stream was not stream-json")
	}
	summary, usage, ferr := clirunFinalize(t, reg)(final)
	if ferr != nil {
		t.Fatalf("finalize: %v", ferr)
	}
	if !strings.Contains(summary, "PONG") {
		t.Fatalf("summary = %q, want PONG", summary)
	}
	t.Logf("PONG ok: tokens %d/%d cost %.4f", usage.TokensIn, usage.TokensOut, usage.CostUSD)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clirunBuild(t *testing.T, reg *clirun.Registry) func(clirun.RunInput) []string {
	t.Helper()
	c, ok := reg.Get("claude")
	if !ok {
		t.Fatal("claude adapter missing")
	}
	return c.BuildArgs
}

func clirunParse(t *testing.T, reg *clirun.Registry) func(io.Reader) <-chan clirun.StreamEvent {
	t.Helper()
	c, ok := reg.Get("claude")
	if !ok {
		t.Fatal("claude adapter missing")
	}
	return c.ParseStream
}

func clirunFinalize(t *testing.T, reg *clirun.Registry) func(string) (string, clirun.Usage, error) {
	t.Helper()
	c, ok := reg.Get("claude")
	if !ok {
		t.Fatal("claude adapter missing")
	}
	return c.Finalize
}
