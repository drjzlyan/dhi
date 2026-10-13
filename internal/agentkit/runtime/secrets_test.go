package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
)

type memSecrets map[string]string

func (m memSecrets) LookupIn(_, name string) (string, bool) {
	v, ok := m[name]
	return v, ok
}

// F-089: a missing secret parks a card marked Missing; once the human
// stores it and says yes, the agent is told $NAME is set — never the
// value — and the next run's env carries it.
func TestSecretRequestNeverReturnsTheValue(t *testing.T) {
	h := episodeHarness(t)
	store := memSecrets{}
	h.rt.cfg.Secrets = store
	ask := h.rt.secretSeam("scout")
	done := make(chan string, 1)
	go func() {
		out, err := ask(context.Background(), "STRIPE_KEY", "charge a test card")
		if err != nil {
			out = "ERR " + err.Error()
		}
		done <- out
	}()
	ap := firstPending(t, h.approvals)
	if !ap.IsSecret() || !ap.Missing || ap.SecretName() != "STRIPE_KEY" {
		t.Fatalf("card = %+v", ap)
	}
	store["STRIPE_KEY"] = "sk_test_value_123" // what the masked form saves
	h.approvals.Decide(ap, "yes")
	out := <-done
	if strings.Contains(out, "sk_test") || !strings.Contains(out, "$STRIPE_KEY") {
		t.Fatalf("result = %q", out)
	}
	if env := h.rt.secretEnv("scout")(); len(env) != 1 || env[0] != "STRIPE_KEY=sk_test_value_123" {
		t.Fatalf("run env = %v", env)
	}
	if h.rt.secretEnv("other")() != nil {
		t.Fatal("another agent got the secret")
	}
	if !strings.Contains(h.rt.secretsLine("scout"), "$STRIPE_KEY") {
		t.Fatal("prompt does not name the granted secret")
	}
	// Granted and stored: no second card.
	again, err := ask(context.Background(), "STRIPE_KEY", "again")
	if err != nil || len(h.approvals.List()) != 0 || !strings.Contains(again, "is set for you") {
		t.Fatalf("second ask = %q %v", again, err)
	}
}

// Saying yes without a value (e.g. the editor sidebar) fails by name.
func TestSecretRequestAllowedWithoutValueFails(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Secrets = memSecrets{}
	errc := make(chan error, 1)
	go func() {
		_, err := h.rt.secretSeam("scout")(context.Background(), "TOKEN", "why")
		errc <- err
	}()
	h.approvals.ResolveFor(firstPending(t, h.approvals).ID, tools.LayerOnce)
	if err := <-errc; err == nil || !strings.Contains(err.Error(), "no value") {
		t.Fatalf("err = %v", err)
	}
}

// Live (M32): the served `run` tool never got the runner, so it always
// answered "command runner unavailable" and secrets could not reach it.
func TestServedToolsGetTheRunner(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Run = &fakeRunner{}
	e, _ := h.rt.agentEntry("scout")
	d := h.rt.toolDeps(e, bus.Message{Channel: "#general"}, turnTools{}, nil, nil)
	if d.Run == nil {
		t.Fatalf("run = %v", d.Run)
	}
}
