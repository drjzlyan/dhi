package dhitools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/setprop"
)

// F-070: settings_propose checks the change, records it pending, and
// @mentions the human; a refused change records nothing.
func TestSettingsPropose(t *testing.T) {
	f, m := newFixture(t)
	store := setprop.Open(f.ws.Root)
	check := func(key, value, layer string) (string, error) {
		if key == "security.sandbox" {
			return "", fmt.Errorf("settings: %q cannot be proposed", key)
		}
		return "0", nil
	}
	h := Deps{Agent: m, WS: f.ws, Bus: f.bus, Channel: "#general", SetProps: store, SettingCheck: check}.Handler()
	out, isErr := call(h, t, "settings_propose", `{"key":"budgets.task_usd","value":"5","rationale":"40-file migration"}`)
	if isErr || !strings.Contains(out, "proposed budgets.task_usd = 5") {
		t.Fatalf("propose = %q", out)
	}
	p, _ := store.Pending()
	if len(p) != 1 || p[0].Old != "0" || p[0].Agent != "scout" || p[0].Layer != setprop.LayerWorkspace {
		t.Fatalf("pending = %+v", p)
	}
	if hist := f.bus.History("#general", 0); len(hist) != 1 || !strings.HasPrefix(hist[0].Text, "@you I propose `budgets.task_usd`: 0 → 5") {
		t.Fatalf("mention = %+v", hist)
	}
	if out, isErr := call(h, t, "settings_propose", `{"key":"security.sandbox","value":"off","rationale":"faster"}`); !isErr || !strings.Contains(out, "cannot be proposed") {
		t.Fatalf("unproposable key must refuse: %q", out)
	}
	if out, isErr := call(h, t, "settings_propose", `{"key":"budgets.task_usd","value":"5","rationale":" "}`); !isErr || !strings.Contains(out, "rationale") {
		t.Fatalf("a rationale is required: %q", out)
	}
	if p, _ := store.Pending(); len(p) != 1 {
		t.Fatal("refused proposals must record nothing")
	}
}

// F-080: project_update proposes one brief field; the brief is untouched.
func TestProjectUpdateProposesABriefField(t *testing.T) {
	f, m := newFixture(t)
	store := setprop.Open(f.ws.Root)
	_ = brief.Save(f.ws.Root, brief.Brief{Process: "old way"})
	h := Deps{Agent: m, WS: f.ws, Bus: f.bus, Channel: "#general", SetProps: store}.Handler()
	out, isErr := call(h, t, "project_update", `{"field":"process","text":"trunk-based","rationale":"interview"}`)
	if isErr || !strings.Contains(out, "proposed project.process") {
		t.Fatalf("propose = %q", out)
	}
	p, _ := store.Pending()
	if len(p) != 1 || p[0].Layer != setprop.LayerProject || p[0].Old != "old way" || p[0].Value != "trunk-based" {
		t.Fatalf("pending = %+v", p)
	}
	if b, _, _ := brief.Load(f.ws.Root); b.Process != "old way" {
		t.Fatal("the brief changed before the human approved")
	}
	if hist := f.bus.History("#general", 0); len(hist) != 1 || !strings.Contains(hist[0].Text, "project brief's process") {
		t.Fatalf("mention = %+v", hist)
	}
	if out, isErr := call(h, t, "project_update", `{"field":"budget","text":"x","rationale":"r"}`); !isErr {
		t.Fatalf("unknown field accepted: %q", out)
	}
}
