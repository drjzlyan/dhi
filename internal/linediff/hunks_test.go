package linediff

import (
	"reflect"
	"strings"
	"testing"
)

func TestHunksAndPartialApply(t *testing.T) {
	older := strings.Split("a\nb\nc\nd\ne", "\n")
	newer := strings.Split("a\nB\nc\nd\ne\nf", "\n")
	hs, ok := Hunks(older, newer)
	if !ok || len(hs) != 2 {
		t.Fatalf("hunks = %+v", hs)
	}
	if hs[0].OldAt != 1 || !reflect.DeepEqual(hs[0].Old, []string{"b"}) || !reflect.DeepEqual(hs[0].New, []string{"B"}) {
		t.Fatalf("hunk 0 = %+v", hs[0])
	}
	if got := Apply(older, hs, []bool{true, true}); !reflect.DeepEqual(got, newer) {
		t.Fatalf("all = %v", got)
	}
	if got := Apply(older, hs, []bool{false, true}); strings.Join(got, "") != "abcdef" {
		t.Fatalf("second only = %v", got)
	}
	if got := Apply(older, hs, []bool{false, false}); !reflect.DeepEqual(got, older) {
		t.Fatalf("none = %v", got)
	}
}
