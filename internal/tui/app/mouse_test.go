package app

import (
	"testing"

	"charm.land/bubbletea/v2"
)

// mouseSurface records wheel/click seams (F-026 P2).
type mouseSurface struct {
	stubSurface
	dys   []int
	click [2]int
}

func (s *mouseSurface) Wheel(dy int) bool { s.dys = append(s.dys, dy); return true }
func (s *mouseSurface) Click(x, y int) bool {
	s.click = [2]int{x, y}
	return true
}

func newMouseApp(t *testing.T) (*App, *mouseSurface) {
	t.Helper()
	ms := &mouseSurface{stubSurface: stubSurface{id: "mouse", title: "Mouse", consumeKeys: true}}
	home := &stubSurface{id: "home", title: "Workspace"}
	a := New("test", ms, home)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return a, ms
}

func TestWheelRoutesToActiveSurface(t *testing.T) {
	a, ms := newMouseApp(t)
	a.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	a.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if len(ms.dys) != 2 || ms.dys[0] != -1 || ms.dys[1] != 1 {
		t.Fatalf("dys = %v, want [-1 1]", ms.dys)
	}
}

func TestTabBarClickSelectsSurface(t *testing.T) {
	a, _ := newMouseApp(t)
	// Tab 2 (" Mouse ") starts after the brand + tab 1 + separator:
	// x=22 lands inside it (Tabs.Hit contract).
	a.Update(tea.MouseClickMsg(tea.Mouse{X: 22, Y: 0}))
	if a.active != 1 {
		t.Fatalf("tab click active = %d, want 1", a.active)
	}
	// Row 0 misses every tab → no change.
	a.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 0}))
	if a.active != 1 {
		t.Fatalf("brand click changed surface")
	}
}

func TestSurfaceClickReceivesBodyLocalCoords(t *testing.T) {
	a, ms := newMouseApp(t)
	// Row 1 of the screen = row 0 of the body.
	a.Update(tea.MouseClickMsg(tea.Mouse{X: 4, Y: 1}))
	if ms.click != [2]int{4, 0} {
		t.Fatalf("click coords = %v, want [4 0]", ms.click)
	}
	// Statusline row: no action, no coords delivered.
	ms.click = [2]int{-1, -1}
	a.Update(tea.MouseClickMsg(tea.Mouse{X: 4, Y: a.height - 1}))
	if ms.click != [2]int{-1, -1} {
		t.Fatalf("statusline click routed: %v", ms.click)
	}
}

func TestHelpClosesOnClick(t *testing.T) {
	a, ms := newMouseApp(t)
	a.Update(keyPress("?"))
	if !a.showHelp {
		t.Fatal("help not open")
	}
	a.Update(tea.MouseClickMsg(tea.Mouse{X: 10, Y: 10}))
	if a.showHelp {
		t.Fatal("help click did not close")
	}
	if ms.click != [2]int{} {
		t.Fatalf("surface click routed under help: %v", ms.click)
	}
}
