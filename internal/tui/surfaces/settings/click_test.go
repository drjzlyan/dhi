package settings

import "testing"

func TestRailClickJumpsToSectionButNotThroughDialogs(t *testing.T) {
	m, _ := newSurface(t)
	m.Resize(110, 30)
	if !m.Click(3, int(secTeams)) {
		t.Fatal("a click on a rail row should be handled")
	}
	if m.sec != secTeams {
		t.Fatalf("sec = %v, want teams", m.sec)
	}
	if m.Click(60, 2) {
		t.Fatal("a click in the pane is not a rail click")
	}
	if m.Click(3, int(secCount)+2) {
		t.Fatal("a click below the last row hits nothing")
	}
	m.form.open = true
	if m.Click(3, int(secAgents)) || m.sec != secTeams {
		t.Fatal("an open form must swallow rail clicks")
	}
}
