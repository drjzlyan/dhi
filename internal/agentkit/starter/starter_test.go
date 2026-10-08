package starter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func fixture(t *testing.T) (*workspace.Workspace, *org.Org, *library.Store) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	if err := workspace.Create(root, "api"); err != nil {
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
	return ws, o, library.Open(ws)
}

func TestEveryTemplateParsesAndResolvesAgainstTheBuiltinLibrary(t *testing.T) {
	entries, _ := templatesFS.ReadDir("templates")
	if len(entries) < 3 {
		t.Fatalf("only %d template files", len(entries))
	}
	lib := library.Open(nil)
	if got := len(Templates()); got != len(entries) {
		t.Fatalf("Templates() = %d of %d files — a template fails to parse", got, len(entries))
	}
	for _, tpl := range Templates() {
		ids := map[string]bool{}
		for _, e := range tpl.Employees {
			if ids[e.ID] {
				t.Errorf("%s: duplicate %s", tpl.Slug, e.ID)
			}
			ids[e.ID] = true
			if _, err := build(lib, e); err != nil {
				t.Errorf("%s: %v", tpl.Slug, err)
			}
		}
		if tpl.Lead != org.Human && !ids[tpl.Lead] {
			t.Errorf("%s: lead %q not an employee", tpl.Slug, tpl.Lead)
		}
	}
}

func TestTemplatesAreOrderedSmallestFirst(t *testing.T) {
	var slugs []string
	for _, tpl := range Templates() {
		slugs = append(slugs, tpl.Slug)
	}
	if strings.Join(slugs, ",") != "solo,squad,studio" {
		t.Fatalf("order = %v", slugs)
	}
	if _, ok := Get("squad"); !ok {
		t.Fatal("Get(squad) failed")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("Get(nope) succeeded")
	}
}

func TestApplyWritesAgentsWithRolePolicyAndTeam(t *testing.T) {
	ws, o, lib := fixture(t)
	tpl, _ := Get("squad")
	res, err := Apply(ws, o, lib, tpl)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 4 || len(res.Skipped) != 0 || !res.TeamCreated || res.Team != "squad" {
		t.Fatalf("result = %+v", res)
	}
	roster, err := org.LoadRoster(ws)
	if err != nil || len(roster) != 4 {
		t.Fatalf("roster = %d err=%v", len(roster), err)
	}
	byID := map[string]*manifest.Agent{}
	for _, a := range roster {
		byID[a.ID] = a
	}
	atlas := byID["atlas"]
	if atlas.Role != "planner" || atlas.Persona != "mentor" || atlas.Model != manifest.ModelDefault {
		t.Fatalf("atlas = %+v", atlas)
	}
	if atlas.Engine != "" {
		t.Fatalf("starter employees must inherit the workspace engine, got %q", atlas.Engine)
	}
	// Tools and policy come from the ROLE.
	planner, _ := lib.Role("planner")
	if strings.Join(atlas.Tools, ",") != strings.Join(planner.Tools, ",") {
		t.Fatalf("tools %v != role %v", atlas.Tools, planner.Tools)
	}
	if p := atlas.Policy(); p == nil {
		t.Fatal("read-only role must install a policy")
	}
	if forge := byID["forge"]; forge.Policy() == nil || forge.Role != "fixer" {
		t.Fatalf("forge = %+v", forge)
	}
	team, ok := o.Team("squad")
	if !ok || team.Lead != "atlas" || len(team.Members) != 4 {
		t.Fatalf("team = %+v ok=%v", team, ok)
	}
}

func TestApplyIsIdempotentAndNeverOverwrites(t *testing.T) {
	ws, o, lib := fixture(t)
	tpl, _ := Get("solo")
	if _, err := Apply(ws, o, lib, tpl); err != nil {
		t.Fatal(err)
	}
	// Hand-edit an employee; a re-run must leave it alone.
	path := filepath.Join(ws.Root, workspace.DirAgents, "forge.toml")
	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), `name = "Forge"`, `name = "Forge (edited)"`, 1)
	os.WriteFile(path, []byte(edited), 0o644)

	res, err := Apply(ws, o, lib, tpl)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 0 || len(res.Skipped) != 2 || res.TeamCreated {
		t.Fatalf("re-run = %+v", res)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "Forge (edited)") {
		t.Fatal("re-run overwrote a hand-edited employee")
	}
}

func TestApplyRefusesArchivedAndWritesNothing(t *testing.T) {
	ws, o, lib := fixture(t)
	tpl, _ := Get("solo")
	if _, err := Apply(ws, o, lib, tpl); err != nil {
		t.Fatal(err)
	}
	if err := o.ArchiveAgent(ws, "sage"); err != nil {
		t.Fatal(err)
	}
	squad, _ := Get("squad")
	_, err := Apply(ws, o, lib, squad)
	if err == nil || !strings.Contains(err.Error(), "sage") || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("err = %v", err)
	}
	roster, _ := org.LoadRoster(ws)
	if len(roster) != 1 { // only forge from solo; nothing new from squad
		t.Fatalf("a refused apply wrote %d agents", len(roster))
	}
}

func TestApplyRollsBackWhenTheTeamCannotBeCreated(t *testing.T) {
	ws, o, lib := fixture(t)
	bad := Template{Slug: "bad", Name: "Bad", Description: "d", Team: "Bad Slug", Lead: org.Human,
		Employees: []Employee{{ID: "forge", Name: "Forge", Role: "fixer"}}}
	if _, err := Apply(ws, o, lib, bad); err == nil {
		t.Fatal("expected a team error")
	}
	if roster, _ := org.LoadRoster(ws); len(roster) != 0 {
		t.Fatalf("rollback left %d agent(s) behind", len(roster))
	}
}

func TestApplyValidatesBeforeTheFirstWrite(t *testing.T) {
	ws, o, lib := fixture(t)
	bad := Template{Slug: "bad", Name: "Bad", Description: "d", Team: "t", Lead: org.Human,
		Employees: []Employee{
			{ID: "forge", Name: "Forge", Role: "fixer"},
			{ID: "ghost", Name: "Ghost", Role: "necromancer"},
		}}
	_, err := Apply(ws, o, lib, bad)
	if err == nil || !strings.Contains(err.Error(), "necromancer") {
		t.Fatalf("err = %v", err)
	}
	if roster, _ := org.LoadRoster(ws); len(roster) != 0 {
		t.Fatalf("validation failure still wrote %d agent(s)", len(roster))
	}
}

func TestParseRejectsBrokenTemplates(t *testing.T) {
	bad := map[string]string{
		"unknown key": "schema = 1\nname=\"n\"\ndescription=\"d\"\nteam=\"t\"\nlead=\"you\"\nmood=\"x\"\n[[employee]]\nid=\"a\"\nname=\"A\"\nrole=\"fixer\"\n",
		"no employee": "schema = 1\nname=\"n\"\ndescription=\"d\"\nteam=\"t\"\nlead=\"you\"\n",
		"bad lead":    "schema = 1\nname=\"n\"\ndescription=\"d\"\nteam=\"t\"\nlead=\"zed\"\n[[employee]]\nid=\"a\"\nname=\"A\"\nrole=\"fixer\"\n",
		"duplicate":   "schema = 1\nname=\"n\"\ndescription=\"d\"\nteam=\"t\"\nlead=\"a\"\n[[employee]]\nid=\"a\"\nname=\"A\"\nrole=\"fixer\"\n[[employee]]\nid=\"a\"\nname=\"B\"\nrole=\"fixer\"\n",
	}
	for name, doc := range bad {
		if _, err := parse("x", []byte(doc)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
