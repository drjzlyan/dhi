package gitcore

import (
	"strings"
	"testing"
)

func TestPushGuardEnvAppendsAfterExistingConfig(t *testing.T) {
	env := PushGuardEnv([]string{"PATH=/bin", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=a.b", "GIT_CONFIG_VALUE_0=c"}, "/g")
	got := strings.Join(env, " ")
	for _, want := range []string{"GIT_CONFIG_KEY_0=a.b", "GIT_CONFIG_KEY_1=hook.dhi-default-branch.event", "GIT_CONFIG_VALUE_2=/g", "GIT_CONFIG_COUNT=3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if strings.Count(got, "GIT_CONFIG_COUNT=") != 1 {
		t.Fatal("two counts")
	}
}
