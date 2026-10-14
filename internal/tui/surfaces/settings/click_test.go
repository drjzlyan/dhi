package settings

import "testing"

func TestRailClickJumpsToSectionButNotThroughDialogs(t *testing.T) {
	m, _ := newSurface(t)
	m.Resize(110, 30)
	// Rows sit under group headings (F-093): TEAMS is the first row of
	// the third group, so three headings precede it.
	if !m.Click(3, int(secTeams)+3) {
		t.Fatal("a click on a rail row should be handled")
	}
	if m.sec != secTeams {
		t.Fatalf("sec = %v, want teams", m.sec)
	}
	if m.Click(60, 2) {
		t.Fatal("a click in the pane is not a rail click")
	}
	if m.Click(3, 0) {
		t.Fatal("a click on a group heading selects nothing")
	}
	if m.Click(3, int(secCount)+5+2) {
		t.Fatal("a click below the last row hits nothing")
	}
	m.form.open = true
	if m.Click(3, int(secAgents)+2) || m.sec != secTeams {
		t.Fatal("an open form must swallow rail clicks")
	}
}

// F-064: CONFIG is grouped under headers, so a click maps through the
// rendered lines — a header hits nothing, a setting row selects itself.
func TestConfigClickMapsThroughGroupHeaders(t *testing.T) {
	m, _ := newSurface(t)
	m.Resize(110, 40)
	m.sec = secConfig
	_ = m.View()
	if m.Click(40, 1) { // line 0: the APPEARANCE header
		t.Fatal("a click on a group header must not select a setting")
	}
	line := -1
	for i, r := range m.cfgRowAt {
		if r == rowTabWidth {
			line = i
		}
	}
	if line < 0 {
		t.Fatal("editor.tab_width not rendered")
	}
	if !m.Click(40, line+1) || m.cursor != rowTabWidth {
		t.Fatalf("cursor = %d, want editor.tab_width (%d)", m.cursor, rowTabWidth)
	}
}
