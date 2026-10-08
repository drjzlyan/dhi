package editor

import (
	"strings"
	"testing"
)

// TestTreeClicksSelectThenOpen pins F-055: click a tree row to select
// it, click it again to expand a folder or open a file.
func TestTreeClicksSelectThenOpen(t *testing.T) {
	m := newEditor(t)
	rowOf := func(name string) int {
		for y, l := range strings.Split(plainView(m), "\n") {
			if i := strings.Index(l, name); i >= 0 && i < m.railW() {
				return y
			}
		}
		t.Fatalf("%q not in the tree:\n%s", name, plainView(m))
		return -1
	}
	if !m.Click(3, rowOf("beta/")) || m.list.Cursor != 1 {
		t.Fatalf("click on beta/: cursor %d", m.list.Cursor)
	}
	y := rowOf("alpha/")
	m.Click(3, y) // select
	m.Click(3, y) // second click on the selected folder expands it
	y = rowOf("app.go")
	m.Click(5, y)
	m.Click(5, y)
	if m.active() == nil || !strings.HasSuffix(m.active().Path(), "app.go") {
		t.Fatalf("double click did not open app.go:\n%s", plainView(m))
	}
	if m.Click(m.width-2, 3) {
		t.Fatal("a click outside the tree must not be handled")
	}
}
