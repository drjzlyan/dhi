package starter

import (
	"reflect"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
)

func TestEverySlotRoleResolvesInTheBuiltinLibrary(t *testing.T) {
	lib := library.Open(nil)
	for _, s := range Slots {
		for n := 0; n < 3; n++ {
			e := Employee{ID: "x", Name: "X", Role: s.Role, Persona: PersonaFor(s.Role, n), Skills: roleSkills[s.Role]}
			if _, err := build(lib, e); err != nil {
				t.Errorf("%s #%d: %v", s.Role, n, err)
			}
		}
	}
}

func roles(t Template) []string {
	var out []string
	for _, e := range t.Employees {
		out = append(out, e.Role)
	}
	return out
}

func TestComposeOneTeamPerLeadDealsTheRestRoundRobin(t *testing.T) {
	tpls, err := Compose(Counts{"lead": 2, "fullstack": 3, "tester": 1, "product-manager": 1}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) != 2 {
		t.Fatalf("%d teams, want 2", len(tpls))
	}
	if got := roles(tpls[0]); !reflect.DeepEqual(got, []string{"lead", "fullstack", "fullstack", "product-manager"}) {
		t.Errorf("team 1 roles %v", got)
	}
	if got := roles(tpls[1]); !reflect.DeepEqual(got, []string{"lead", "fullstack", "tester"}) {
		t.Errorf("team 2 roles %v", got)
	}
	if tpls[0].Lead != tpls[0].Employees[0].ID || tpls[0].Team != tpls[0].Lead+"-team" {
		t.Errorf("team 1 lead %q slug %q", tpls[0].Lead, tpls[0].Team)
	}
	ids := map[string]bool{}
	for _, tpl := range tpls {
		for _, e := range tpl.Employees {
			if ids[e.ID] {
				t.Errorf("duplicate id %s", e.ID)
			}
			ids[e.ID] = true
		}
	}
}

func TestComposeWithoutLeadIsOneTeamLedByYou(t *testing.T) {
	tpls, err := Compose(Counts{"backend": 2}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) != 1 || tpls[0].Lead != org.Human || tpls[0].Team != "team" {
		t.Fatalf("got %+v", tpls)
	}
}

func TestComposeSkipsTakenIdsAndTeams(t *testing.T) {
	takenIDs := map[string]bool{"atlas": true, "sage": true}
	tpls, err := Compose(Counts{"lead": 1, "frontend": 2}, Options{
		AgentTaken: func(id string) bool { return takenIDs[id] },
		TeamTaken:  func(s string) bool { return s == "forge-team" },
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range tpls[0].Employees {
		got = append(got, e.ID)
	}
	if !reflect.DeepEqual(got, []string{"forge", "quill", "scout"}) {
		t.Errorf("ids %v", got)
	}
	if tpls[0].Team != "forge-team-2" {
		t.Errorf("team %q", tpls[0].Team)
	}
}

func TestComposeNamePoolNeverRunsOut(t *testing.T) {
	tpls, err := Compose(Counts{"fullstack": 9, "backend": 9, "frontend": 9, "tester": 9}, Options{
		AgentTaken: func(id string) bool { return !strings.ContainsAny(id, "0123456789") && id < "s" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(tpls[0].Employees); n != 36 {
		t.Fatalf("%d employees", n)
	}
}

func TestComposeRefusesBadCounts(t *testing.T) {
	for name, c := range map[string]Counts{
		"empty":    {},
		"unknown":  {"wizard": 1},
		"negative": {"tester": -1},
		"too many": {"tester": MaxPerRole + 1},
		"total":    {"fullstack": 9, "backend": 9, "frontend": 9, "tester": 9, "devops": 9},
	} {
		if _, err := Compose(c, Options{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := Compose(Counts{"lead": 1}, Options{Into: "core"}); err == nil {
		t.Error("lead into an existing team: no error")
	}
}

func TestComposePersonaOverride(t *testing.T) {
	tpls, _ := Compose(Counts{"lead": 1, "tester": 1}, Options{Persona: "terse"})
	for _, e := range tpls[0].Employees {
		if e.Persona != "terse" {
			t.Errorf("%s persona %q", e.ID, e.Persona)
		}
	}
}

func TestCountsSummary(t *testing.T) {
	if got := (Counts{"lead": 2, "fullstack": 5}).Summary(); got != "2 teams · 7 employees" {
		t.Errorf("got %q", got)
	}
	if got := (Counts{"tester": 1}).Summary(); got != "1 team · 1 employee" {
		t.Errorf("got %q", got)
	}
}

func TestApplyAllCreatesEveryTeam(t *testing.T) {
	ws, o, lib := fixture(t)
	tpls, _ := Compose(Counts{"lead": 2, "fullstack": 2}, Options{})
	res, err := ApplyAll(ws, o, lib, tpls)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("%d results", len(res))
	}
	for _, tpl := range tpls {
		tm, ok := o.Team(tpl.Team)
		if !ok || tm.Lead != tpl.Lead || len(tm.Members) != 2 {
			t.Errorf("team %s: %+v", tpl.Team, tm)
		}
	}
}

func TestApplyAllRollsBackEarlierTeams(t *testing.T) {
	ws, o, lib := fixture(t)
	tpls, _ := Compose(Counts{"lead": 2}, Options{})
	tpls[1].Employees[0].Role = "no-such-role"
	if _, err := ApplyAll(ws, o, lib, tpls); err == nil {
		t.Fatal("no error")
	}
	if len(o.Teams()) != 0 {
		t.Errorf("teams left: %v", o.Teams())
	}
	roster, _ := org.LoadRoster(ws)
	if len(roster) != 0 {
		t.Errorf("%d agents left", len(roster))
	}
}

func TestApplyAllIntoExistingTeamAddsMembers(t *testing.T) {
	ws, o, lib := fixture(t)
	first, _ := Compose(Counts{"lead": 1}, Options{})
	if _, err := ApplyAll(ws, o, lib, first); err != nil {
		t.Fatal(err)
	}
	team := first[0].Team
	more, err := Compose(Counts{"tester": 2}, Options{Into: team,
		AgentTaken: func(id string) bool { return id == first[0].Lead }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAll(ws, o, lib, more); err != nil {
		t.Fatal(err)
	}
	tm, _ := o.Team(team)
	if len(tm.Members) != 3 || tm.Lead != first[0].Lead {
		t.Errorf("team after add: %+v", tm)
	}
}
