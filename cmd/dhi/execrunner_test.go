package main

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

func TestExecRunnerGrowsByGrants(t *testing.T) {
	sb, err := sandbox.NewSeatbelt("/usr/bin/sandbox-exec", []string{"/w"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := execRunner{timeout: time.Minute, sandbox: sb}
	g := r.WithGrants([]string{"/granted/out"}, []string{"/opt/tool"}).(execRunner)
	p := g.sandbox.(*sandbox.Seatbelt).Profile()
	for _, want := range []string{`(subpath "/granted/out")`, `(subpath "/opt/tool")`} {
		if !strings.Contains(p, want) {
			t.Errorf("profile lacks %s", want)
		}
	}
	if strings.Contains(r.sandbox.(*sandbox.Seatbelt).Profile(), "/granted/out") {
		t.Fatal("the shared runner must not change")
	}
}
