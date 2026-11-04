package behavior

import (
	"strings"
	"testing"
)

// F-144: the default mode adds nothing; each read-only mode appends one block.
func TestComposeModes(t *testing.T) {
	in := Input{AgentID: "a", ManifestSystem: "You scout."}
	base := Compose(in)
	if base != "You scout." {
		t.Fatalf("default = %q", base)
	}
	for _, mode := range []string{ModePlan, ModeReview, ModeAsk} {
		in.Mode = mode
		got := Compose(in)
		if !strings.HasPrefix(got, base+"\n\nMODE: "+mode+".") || !ModeReadOnly(mode) {
			t.Errorf("%s = %q", mode, got)
		}
	}
	if ModeReadOnly("") || ModeReadOnly("write") {
		t.Error("only plan, review and ask are read-only")
	}
}
