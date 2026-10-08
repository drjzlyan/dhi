package kit

import "testing"

func TestHitMapResolvesZonesWithOrigins(t *testing.T) {
	var m HitMap
	var got [3]int
	m.Add(0, 0, 10, 2, func(dx, dy int) { got = [3]int{1, dx, dy} })
	m.SetOrigin(20, 5)
	m.Add(2, 1, 4, 3, func(dx, dy int) { got = [3]int{2, dx, dy} })
	m.Add(2, 1, 1, 1, func(dx, dy int) { got = [3]int{3, dx, dy} }) // later zone on top

	if !m.Click(3, 1) || got != [3]int{1, 3, 1} {
		t.Fatalf("zone 1: %v", got)
	}
	if !m.Click(24, 8) || got != [3]int{2, 2, 2} {
		t.Fatalf("zone 2 relative to origin: %v", got)
	}
	if !m.Click(22, 6) || got != [3]int{3, 0, 0} {
		t.Fatalf("topmost zone must win: %v", got)
	}
	if m.Click(50, 50) {
		t.Fatal("click outside every zone was handled")
	}
	m.Reset()
	if m.Click(3, 1) {
		t.Fatal("Reset must drop zones")
	}
}
