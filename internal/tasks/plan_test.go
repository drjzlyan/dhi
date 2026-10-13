package tasks

import "testing"

// F-071: decomposition — parent, dependencies, cycles refused, held
// cards become ready when their dependencies finish.
func TestPlanDependenciesAndReady(t *testing.T) {
	s, ws := setupStore(t)
	for _, slug := range []string{"epic", "schema", "api", "ui"} {
		if err := s.Create(slug, slug, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetPlan("schema", "epic", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlan("api", "epic", []string{"schema"}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlan("ui", "epic", []string{"api"}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlan("schema", "epic", []string{"ui"}, false); err == nil {
		t.Fatal("a cycle must refuse")
	}
	if err := s.SetPlan("api", "nope", nil, false); err == nil {
		t.Fatal("an unknown parent must refuse")
	}
	if got := len(s.Children("epic")); got != 3 {
		t.Fatalf("children = %d", got)
	}
	if r := s.Ready(); len(r) != 0 {
		t.Fatalf("nothing is ready yet: %+v", r)
	}
	if err := s.SetStatus("schema", Done); err != nil {
		t.Fatal(err)
	}
	r := s.Ready()
	if len(r) != 1 || r[0].Slug != "api" {
		t.Fatalf("ready = %+v", r)
	}
	if err := s.Release("api"); err != nil {
		t.Fatal(err)
	}
	if len(s.Ready()) != 0 || len(s.Blockers("ui")) != 1 {
		t.Fatal("released card must leave the ready set; ui still waits on api")
	}
	// Round trip through disk.
	re, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := re.Get("ui"); got.Parent != "epic" || len(got.DependsOn) != 1 || !got.Held {
		t.Fatalf("reloaded = %+v", got)
	}
}
