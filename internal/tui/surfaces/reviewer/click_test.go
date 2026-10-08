package reviewer

import "testing"

func TestRailClickJumpsToSection(t *testing.T) {
	m, _, _, _ := newSurface(t)
	m.Resize(110, 30)
	if !m.Click(3, int(secDiff)) {
		t.Fatal("a click on a rail row should be handled")
	}
	if m.sec != secDiff {
		t.Fatalf("sec = %v, want diff", m.sec)
	}
	if m.Click(60, 2) {
		t.Fatal("a click in the pane is not a rail click")
	}
	m.Resize(70, 30)
	if m.Click(3, 0) {
		t.Fatal("narrow layout has no rail to click")
	}
}
