package kit

import (
	"reflect"
	"testing"
)

func TestHintRows(t *testing.T) {
	got := HintRows("h/l lane", "[ ] sections", "enter/o jump", "ctrl+s write", "solo", "M bulk move")
	want := [][2]string{
		{"h/l", "lane"},
		{"[ ]", "sections"},
		{"enter/o", "jump"},
		{"ctrl+s", "write"},
		{"solo", ""},
		{"M", "bulk move"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HintRows =\n%q\nwant\n%q", got, want)
	}
}

func TestDedupeHelpRows(t *testing.T) {
	got := DedupeHelpRows([][2]string{
		{"[ / ]", "switch sections"},
		{"ctrl+s", "write settings"},
		{"[ ]", "sections"},
		{"ctrl+s", "write"},
		{"n", "new"},
	})
	want := [][2]string{{"[ / ]", "switch sections"}, {"ctrl+s", "write settings"}, {"n", "new"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dedupe = %q, want %q", got, want)
	}
}

func TestHintKeyAt(t *testing.T) {
	hints := []string{"h/l lane", "n new", "[ ] sections", "space mark"}
	// "h/l lane • n new • [ ] sections • space mark"
	cases := map[int]string{0: "h", 7: "h", 11: "n", 19: "[", 34: " "}
	for x, want := range cases {
		if got, ok := HintKeyAt(80, "", x, hints...); !ok || got != want {
			t.Errorf("x=%d: %q,%v want %q", x, got, ok, want)
		}
	}
	if _, ok := HintKeyAt(80, "", 9, hints...); ok {
		t.Error("a separator is not a key")
	}
	// A status segment shifts the hints right by its width + 2.
	if got, ok := HintKeyAt(80, "ok", 4, hints...); !ok || got != "h" {
		t.Errorf("with status: %q,%v", got, ok)
	}
}
