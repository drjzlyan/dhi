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
