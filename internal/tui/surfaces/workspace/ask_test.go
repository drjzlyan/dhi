package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// park queues one sandbox-access ask and returns its verdict channel.
func park(t *testing.T, a *tools.Approvals, agent, path string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- a.AskScope(context.Background(), agent, tools.AccessWrite+path, sandbox.OpWrite, path, "the export goes there")
	}()
	deadline := time.After(5 * time.Second)
	for len(a.List()) == 0 {
		select {
		case <-a.Changes():
		case <-deadline:
			t.Fatal("ask never parked")
		}
	}
	return done
}

// F-087: an agent waiting on an ask shows it in its chat with the keys;
// y grants it for the workspace (a sandbox root is useless for one call).
func TestChatPaneShowsAndAnswersTheAsk(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.approvals = tools.NewApprovals()
	p.buildChannels([]string{"anvil"}, nil)
	p.openNamed("dm:anvil")
	done := park(t, p.approvals, "anvil", "/data/out")

	view := ansi.Strip(strings.Join(p.render(220, 20), "\n"))
	for _, want := range []string{"anvil asks: write access to /data/out", "the export goes there", "y yes", "g everywhere"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ask strip missing %q:\n%s", want, view)
		}
	}
	if !p.handleKey("y") {
		t.Fatal("y not handled")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := p.approvals.Targets("anvil"); len(got) != 1 || got[0] != "/data/out" {
		t.Fatalf("grant = %v", got)
	}
	if gs := p.approvals.Grants(); gs[0].Layer != tools.LayerWorkspace {
		t.Fatalf("layer = %s", gs[0].Layer)
	}
}

// Another agent's ask stays out of a DM; the composer keeps its letters.
func TestChatPaneAskIsScopedAndComposerSafe(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.approvals = tools.NewApprovals()
	p.buildChannels([]string{"anvil", "forge"}, nil)
	p.openNamed("dm:forge")
	done := park(t, p.approvals, "anvil", "/data/out")
	if strings.Contains(strings.Join(p.render(100, 20), "\n"), "asks:") {
		t.Fatal("anvil's ask shown in forge's DM")
	}
	p.openNamed("dm:anvil")
	p.focus = true
	p.handleKey("n")
	if len(p.approvals.List()) != 1 {
		t.Fatal("typing n in the composer denied the ask")
	}
	p.focus = false
	p.handleKey("n")
	if err := <-done; err == nil {
		t.Fatal("n did not deny")
	}
}
