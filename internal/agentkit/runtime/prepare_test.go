package runtime

import (
	"context"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/tasks"
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

// fakeToolchain is a host-tool seam with a fixed set of missing tools.
type fakeToolchain struct{ missing map[string]string }

func (f *fakeToolchain) Missing(name string) (string, bool) {
	hint, ok := f.missing[name]
	return hint, ok
}

// A project whose toolchain is not installed stays unverified, named,
// with the install command; nothing is installed and nothing asks.
func TestVerifyNamesAMissingToolchain(t *testing.T) {
	nr := &netRunner{}
	ap := tools.NewApprovals()
	r := &Runtime{cfg: Config{Run: nr, Toolchain: &fakeToolchain{missing: map[string]string{"rust": "brew install rust"}}, Approvals: ap}}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0o644)
	p := verifyplan.Detect(dir)[0]
	tg := verifyTarget{dir: dir, argv: p.Test, plan: p}

	res := r.verify(context.Background(), "scout", scopes.Set{scopes.Network: scopes.Auto}, []verifyTarget{tg})
	if res.ok || !res.env || !strings.Contains(res.tail, "rust toolchain is not installed") ||
		!strings.Contains(res.tail, "brew install rust") || len(nr.calls) != 0 || len(ap.List()) != 0 {
		t.Fatalf("res=%+v calls=%v asks=%d", res, nr.calls, len(ap.List()))
	}

	r.cfg.Toolchain = &fakeToolchain{} // installed now
	if res := r.verify(context.Background(), "scout", scopes.Set{scopes.Network: scopes.Auto}, []verifyTarget{tg}); !res.ok {
		t.Fatalf("installed: %+v", res)
	}
}

func TestCollectEpisodesKeepsLiveWork(t *testing.T) {
	h, st := teamHarness(t)
	eps := h.rt.cfg.Episodes
	old := time.Now().Add(-40 * 24 * time.Hour)
	for _, slug := range []string{"live", "done", "gone"} {
		if slug != "gone" {
			if err := st.Create(slug, slug, "scout", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = st.SetStatus("done", tasks.Done)
	for _, slug := range []string{"live", "done", "gone"} {
		_ = eps.Update("scout", episode.Key(slug, "", 0), func(e *episode.Episode) { e.Updated = old })
	}
	h.rt.collectEpisodes("scout")
	left, _ := eps.List("scout")
	if len(left) != 1 || left[0].Key != "task:live" {
		t.Fatalf("left = %+v", left)
	}
}

func TestMemoryTuningDefaults(t *testing.T) {
	mt := Tuning{}.memoryTuning()
	if mt.PromptTokens != 24000 || mt.SessionTokens != 160000 || mt.JournalMax != 400 || mt.DecayDays != 30 {
		t.Fatalf("defaults = %+v", mt)
	}
	if got := (Tuning{Memory: MemoryTuning{PromptTokens: 9000, PinnedMax: 3}}).memoryTuning(); got.PromptTokens != 9000 || got.PinnedMax != 3 {
		t.Fatalf("overrides = %+v", got)
	}
}

// "Allow always" on a verify install must not widen the agent's own
// network grant.
func TestVerifyInstallGrantIsNotNetworkGrant(t *testing.T) {
	nr := &netRunner{}
	ap := tools.NewApprovals()
	r := &Runtime{cfg: Config{Run: nr, Approvals: ap}}
	go func() {
		for i := 0; i < 200; i++ {
			if l := ap.List(); len(l) > 0 {
				ap.ResolveAlways(l[0].ID)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	r.verify(context.Background(), "scout", scopes.Set{scopes.Network: scopes.Ask}, []verifyTarget{nodeTarget(t)})
	for _, g := range ap.Grants() {
		if g.Scope == string(scopes.Network) {
			t.Fatalf("verify install granted the network scope: %+v", g)
		}
	}
	if gs := ap.Grants(); len(gs) != 1 || gs[0].Scope != verifyDepsScope {
		t.Fatalf("grants = %+v", gs)
	}
}
