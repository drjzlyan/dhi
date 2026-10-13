package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/agentkit/verifyplan"
)

// netRunner records commands and whether each had network.
type netRunner struct {
	calls []string
	nets  []bool
}

func (n *netRunner) Run(_ context.Context, _ string, argv []string, net bool) (string, error) {
	n.calls = append(n.calls, strings.Join(argv, " "))
	n.nets = append(n.nets, net)
	return "", nil
}

func nodeTarget(t *testing.T) verifyTarget {
	t.Helper()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"jest"},"dependencies":{"a":"1"}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{}`), 0o644)
	p := verifyplan.Detect(dir)[0]
	return verifyTarget{dir: dir, argv: p.Test, plan: p}
}

func TestVerifyPreparesOncePerLockfile(t *testing.T) {
	nr := &netRunner{}
	r := &Runtime{cfg: Config{Run: nr}}
	tg := nodeTarget(t)
	auto := scopes.Set{scopes.Network: scopes.Auto}
	for i := 0; i < 2; i++ {
		if res := r.verify(context.Background(), "scout", auto, []verifyTarget{tg}); !res.ok {
			t.Fatalf("verify: %+v", res)
		}
	}
	want := "npm ci --no-audit --no-fund|npm test|npm test"
	if got := strings.Join(nr.calls, "|"); got != want {
		t.Fatalf("calls = %s", got)
	}
	if !nr.nets[0] || nr.nets[1] {
		t.Fatalf("network: prepare must have it, the test must not: %v", nr.nets)
	}
	_ = os.WriteFile(filepath.Join(tg.dir, "package-lock.json"), []byte(`{"v":2}`), 0o644)
	r.verify(context.Background(), "scout", auto, []verifyTarget{tg})
	if n := strings.Count(strings.Join(nr.calls, "|"), "npm ci"); n != 2 {
		t.Fatalf("a changed lockfile must prepare again (npm ci ran %d times)", n)
	}
}

func TestVerifyPrepareRespectsNetworkScope(t *testing.T) {
	nr := &netRunner{}
	r := &Runtime{cfg: Config{Run: nr}}
	tg := nodeTarget(t)
	res := r.verify(context.Background(), "scout", scopes.Set{scopes.Network: scopes.Deny}, []verifyTarget{tg})
	if res.ok || !res.env || !strings.Contains(res.tail, "network is denied") || len(nr.calls) != 0 {
		t.Fatalf("deny: %+v calls=%v", res, nr.calls)
	}
	ap := tools.NewApprovals()
	r.cfg.Approvals = ap
	go func() {
		for i := 0; i < 100; i++ {
			if l := ap.List(); len(l) > 0 {
				ap.ResolveAlways(l[0].ID)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	res = r.verify(context.Background(), "scout", scopes.Set{scopes.Network: scopes.Ask}, []verifyTarget{tg})
	if !res.ok || len(nr.calls) != 2 {
		t.Fatalf("ask approved: %+v calls=%v", res, nr.calls)
	}
}
