package dhitools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/ideation"
)

func TestIdeationToolsServed(t *testing.T) {
	for _, s := range []string{"ideation_list", "ideation_read"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}

func ideationFixture(t *testing.T) (*fixture, *manifest.Agent, string, *ideation.Store) {
	t.Helper()
	f, m := newFixture(t, "ideation_list", "ideation_read")
	st, err := ideation.Open(f.ws)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.Create("roadmap", "Q4 roadmap", nil)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(st.DirFor(sess.ID), "plan.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.Scan(sess.ID); err != nil {
		t.Fatal(err)
	}
	return f, m, sess.ID, st
}

func TestIdeationListShowsSessions(t *testing.T) {
	f, m, id, st := ideationFixture(t)
	h := Deps{Agent: m, WS: f.ws, Sessions: st, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "ideation_list", `{}`)
	if isErr || !strings.Contains(out, id) || !strings.Contains(out, "Q4 roadmap") {
		t.Fatalf("ideation_list = %q err=%v", out, isErr)
	}
}

func TestIdeationReadArtifact(t *testing.T) {
	f, m, id, st := ideationFixture(t)
	h := Deps{Agent: m, WS: f.ws, Sessions: st, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "ideation_read", `{"session":"`+id+`","path":"plan.md"}`)
	if isErr || !strings.Contains(out, "# Plan") {
		t.Fatalf("ideation_read = %q err=%v", out, isErr)
	}
}

func TestIdeationReadRefusesUnknownArtifact(t *testing.T) {
	f, m, id, st := ideationFixture(t)
	h := Deps{Agent: m, WS: f.ws, Sessions: st, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "ideation_read", `{"session":"`+id+`","path":"nope.md"}`)
	if !isErr || !strings.Contains(out, "unknown artifact") {
		t.Fatalf("unknown artifact = %q isErr=%v", out, isErr)
	}
}
