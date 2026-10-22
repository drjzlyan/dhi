package assign

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func crew() []*manifest.Agent {
	mk := func(id, role string) *manifest.Agent {
		return &manifest.Agent{ID: id, Name: id, Model: "m", Role: role}
	}
	return []*manifest.Agent{mk("atlas", "lead"), mk("forge", "backend"), mk("quill", "scribe"),
		mk("sage", "reviewer"), mk("ops", "devops"), mk("pixel", "designer"), mk("probe", "tester")}
}

func ids(ps []Pick) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.AgentID)
	}
	return out
}

func has(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// F-107: the recommendation follows what the thing is for, with a reason.
func TestRecommendByWhatItIsFor(t *testing.T) {
	lib := library.Open(nil)
	cases := []struct {
		item      Item
		want, not []string
	}{
		{Item{Kind: KindServer, Name: "atlassian", Description: "Connect to Jira and Confluence: search and create issues, access documentation"},
			[]string{"atlas", "quill"}, []string{"pixel", "ops"}},
		{Item{Kind: KindServer, Name: "slack", Description: "Slack workspace integration: search messages, read channels and threads"},
			[]string{"atlas", "quill"}, []string{"forge", "pixel"}},
		{Item{Kind: KindSkill, Name: "code-review", Description: "A repeatable pass over any diff. Use when reviewing a pull request"},
			[]string{"sage"}, []string{"pixel", "quill"}},
		{Item{Kind: KindSkill, Name: "terraform", Description: "Plan infrastructure changes before deploy"},
			[]string{"ops"}, []string{"quill", "pixel"}},
		{Item{Kind: KindServer, Name: "figma", Description: "Read designs and components"},
			[]string{"pixel"}, []string{"ops"}},
	}
	for _, c := range cases {
		got := Recommend(c.item, crew(), lib, nil)
		for _, w := range c.want {
			if !has(ids(got), w) {
				t.Errorf("%s: want %s in %v", c.item.Name, w, got)
			}
		}
		for _, n := range c.not {
			if has(ids(got), n) {
				t.Errorf("%s: %s should not be recommended: %v", c.item.Name, n, got)
			}
		}
		for _, p := range got {
			if p.Reason == "" {
				t.Errorf("%s: %s has no reason", c.item.Name, p.AgentID)
			}
		}
	}
}

func newWS(t *testing.T) (*workspace.Workspace, *org.Org) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "main"), 0o755)
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	o, err := org.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range crew()[:3] {
		if err := o.CreateAgent(ws, a); err != nil {
			t.Fatal(err)
		}
	}
	return ws, o
}

func TestApplySetsExactlyTheChosenAgents(t *testing.T) {
	ws, o := newWS(t)
	it := Item{Kind: KindServer, Name: "slack"}
	added, removed, err := Apply(ws, o, it, []string{"atlas", "quill"})
	if err != nil || len(added) != 2 || len(removed) != 0 {
		t.Fatalf("%v %v %v", added, removed, err)
	}
	added, removed, err = Apply(ws, o, it, []string{"quill"})
	if err != nil || len(added) != 0 || len(removed) != 1 || removed[0] != "atlas" {
		t.Fatalf("narrowing: %v %v %v", added, removed, err)
	}
	roster, _ := org.LoadRoster(ws)
	if got := Holders(roster, it); len(got) != 1 || got[0] != "quill" {
		t.Fatalf("holders = %v", got)
	}
	if _, _, err := Apply(ws, o, it, []string{"ghost"}); err == nil {
		t.Fatal("an unknown agent must refuse before anything is written")
	}
	roster, _ = org.LoadRoster(ws)
	if got := Holders(roster, it); len(got) != 1 {
		t.Fatalf("a refused apply wrote: %v", got)
	}
}

func TestEffectiveAddsTheAllBotsPolicy(t *testing.T) {
	a := &manifest.Agent{ID: "new", Skills: []string{"docs"}}
	e := Effective(a, Policy{Skills: []string{"pdf", "docs"}, Servers: []string{"slack"}})
	if len(e.Skills) != 2 || !has(e.Tools, ServerTool("slack")) || len(a.Skills) != 1 {
		t.Fatalf("effective %+v (original %+v)", e, a)
	}
}
