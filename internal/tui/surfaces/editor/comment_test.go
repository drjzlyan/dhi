package editor

import "testing"

// F-150: the g-chord is the surface's, so the composed forms are tested
// through HandleKey.
func TestCommentChordsThroughSurface(t *testing.T) {
	m, _ := pairFile(t, "a\nb\nc\nd\n")
	e := m.active()
	feed(m, "g", "c", "c")
	if got := e.Buffer().Text(); got != "// a\nb\nc\nd\n" {
		t.Fatalf("gcc: %q", got)
	}
	feed(m, "j", "g", "c", "j")
	if got := e.Buffer().Text(); got != "// a\n// b\n// c\nd\n" {
		t.Fatalf("gcj: %q", got)
	}
	feed(m, "g", "g", "V", "j", "g", "c")
	if got := e.Buffer().Text(); got != "a\nb\n// c\nd\n" {
		t.Fatalf("visual gc: %q", got)
	}
	feed(m, "j", "j", "ctrl+/")
	if got := e.Buffer().Text(); got != "a\nb\nc\nd\n" {
		t.Fatalf("ctrl+/: %q", got)
	}
	feed(m, "ctrl+_")
	if got := e.Buffer().Text(); got != "a\nb\n// c\nd\n" {
		t.Fatalf("ctrl+_: %q", got)
	}
}
