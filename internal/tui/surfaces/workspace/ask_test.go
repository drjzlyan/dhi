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

type savedSecrets map[string]string

func (s savedSecrets) SetIn(root, name, value string) error {
	s[root+"|"+name] = value
	return nil
}

// F-089: yes on a secret the agent asked for opens a masked field; the
// value goes to the store (never the screen), then the ask is granted.
func TestSecretAskCollectsTheValueMasked(t *testing.T) {
	m, ws := newSurface(t)
	m.approvals = tools.NewApprovals()
	saved := savedSecrets{}
	m.secrets = saved
	done := make(chan error, 1)
	go func() {
		done <- m.approvals.AskSecret(context.Background(), "anvil", "STRIPE_KEY", "charge a test card", true)
	}()
	deadline := time.After(5 * time.Second)
	for len(m.approvals.List()) == 0 {
		select {
		case <-m.approvals.Changes():
		case <-deadline:
			t.Fatal("no ask")
		}
	}
	m.decideAsk(m.approvals.List()[0], "yes")
	if m.form.kind != fSecret {
		t.Fatalf("form = %v", m.form.kind)
	}
	for _, r := range "sk_test_123" {
		m.HandleKey(string(r))
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "sk_test") || !strings.Contains(view, "•••") {
		t.Fatalf("secret visible or not masked:\n%s", view)
	}
	m.HandleKey("enter")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if saved[ws.Root+"|STRIPE_KEY"] != "sk_test_123" {
		t.Fatalf("saved = %v", saved)
	}
	if !m.approvals.Granted("anvil", tools.AccessSecret+"STRIPE_KEY") {
		t.Fatal("not granted for the workspace")
	}
}
