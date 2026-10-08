package version

import (
	"strings"
	"testing"
)

func TestStringCarriesIdentity(t *testing.T) {
	old := [3]string{Version, Commit, Date}
	t.Cleanup(func() { Version, Commit, Date = old[0], old[1], old[2] })
	Version, Commit, Date = "1.2.3", "abc1234", "2026-10-08"
	got := String()
	for _, want := range []string{"dhi 1.2.3", "commit abc1234", "built 2026-10-08", "/"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q lacks %q", got, want)
		}
	}
}
