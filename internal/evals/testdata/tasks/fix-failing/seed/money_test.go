package money

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		cents int64
		n     int
		want  []int64
	}{
		{900, 3, []int64{300, 300, 300}},
		{1000, 3, []int64{334, 333, 333}},
		{5, 4, []int64{2, 1, 1, 1}},
	}
	for _, c := range cases {
		if got := Split(c.cents, c.n); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%d, %d) = %v, want %v", c.cents, c.n, got, c.want)
		}
	}
}
