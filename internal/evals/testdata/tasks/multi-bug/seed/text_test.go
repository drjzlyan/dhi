package text

import (
	"math"
	"testing"
)

func TestTruncate(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 6, "hello…"},
		{"Zoë Ångström", 6, "Zoë Å…"},
	} {
		if got := Truncate(c.in, c.n); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestPercentRounds(t *testing.T) {
	if got := Percent(2, 3); got != 67 {
		t.Errorf("Percent(2, 3) = %d, want 67", got)
	}
	if got := Percent(1, 3); got != 33 {
		t.Errorf("Percent(1, 3) = %d, want 33", got)
	}
}

func TestAverageLargeValues(t *testing.T) {
	xs := []int32{math.MaxInt32, math.MaxInt32, math.MaxInt32 - 3}
	if got := Average(xs); got != math.MaxInt32-1 {
		t.Errorf("Average = %d, want %d", got, math.MaxInt32-1)
	}
}
