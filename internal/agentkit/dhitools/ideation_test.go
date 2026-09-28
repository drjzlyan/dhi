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
	f, m := newFixture(t, "ideation_list", "ideation_read", "session_read",
		"artifact_create", "artifact_edit", "propose_session")
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

func TestIdeationRoundTableToolsServed(t *testing.T) {
	for _, s := range []string{"session_read", "artifact_create", "artifact_edit", "propose_session"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}

func TestSessionReadShowsRoundTable(t *testing.T) {
	f, m, id, st := ideationFixture(t)
	if err := st.SetAgents(id, []string{"scout", "mason"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetModerator(id, "scout"); err != nil {
		t.Fatal(err)
	}
	if err := st.GrantFloor(id, "scout"); err != nil {
		t.Fatal(err)
	}
	h := Deps{Agent: m, WS: f.ws, Sessions: st, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "session_read", `{"session":"`+id+`"}`)
	if isErr {
		t.Fatalf("session_read refused: %s", out)
	}
	for _, want := range []string{"participants: mason, scout", "moderator: scout", "floor: scout", "plan.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("session_read missing %q in:\n%s", want, out)
		}
	}
	if _, isErr := call(h, t, "session_read", `{"session":"ghost"}`); !isErr {
		t.Fatal("session_read accepted an unknown session")
	}
}

func TestArtifactCreateEditLifecycle(t *testing.T) {
	f, m, id, st := ideationFixture(t)
	h := Deps{Agent: m, WS: f.ws, Sessions: st, Approvals: f.approvals}.Handler()

	// create → parks on the Write scope, then records a draft.
	out, isErr := callAsync(h, "artifact_create",
		`{"session":"`+id+`","path":"flow.mmd","content":"graph TD\n  A --> B\n"}`, f)(t)
	if isErr || !strings.Contains(out, "created") {
		t.Fatalf("artifact_create = %q isErr=%v", out, isErr)
	}
	a, ok := st.Artifact(id, "flow.mmd")
	if !ok || a.Status != ideation.StatusDraft || a.Author != "scout" {
		t.Fatalf("created artifact = %+v ok=%v", a, ok)
	}

	// create again refuses (edit is the path).
	if out, isErr := callAsync(h, "artifact_create",
		`{"session":"`+id+`","path":"flow.mmd","content":"x"}`, f)(t); !isErr || !strings.Contains(out, "already exists") {
		t.Fatalf("duplicate create = %q isErr=%v", out, isErr)
	}

	// Approve then edit: content change flips status back to draft.
	if err := st.Approve(id, "flow.mmd"); err != nil {
		t.Fatal(err)
	}
	if out, isErr := callAsync(h, "artifact_edit",
		`{"session":"`+id+`","path":"flow.mmd","content":"graph TD\n  A --> B --> C\n"}`, f)(t); isErr || !strings.Contains(out, "updated") {
		t.Fatalf("artifact_edit = %q isErr=%v", out, isErr)
	}
	a, _ = st.Artifact(id, "flow.mmd")
	if a.Status != ideation.StatusDraft || a.Author != "scout" {
		t.Fatalf("edited artifact status = %+v", a)
	}

	// edit on a missing file refuses.
	if out, isErr := callAsync(h, "artifact_edit",
		`{"session":"`+id+`","path":"nope.md","content":"x"}`, f)(t); !isErr || !strings.Contains(out, "not found") {
		t.Fatalf("edit missing = %q isErr=%v", out, isErr)
	}
}

func TestProposeSessionRecordsPending(t *testing.T) {
	f, m, _, st := ideationFixture(t)
	h := Deps{Agent: m, WS: f.ws, Sessions: st, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "propose_session",
		`{"name":"New canvas","topic":"sketch","mode":"group","participants":["scout","mason"]}`)
	if isErr || !strings.Contains(out, "proposed") {
		t.Fatalf("propose_session = %q isErr=%v", out, isErr)
	}
	pend := st.PendingProposals()
	if len(pend) != 1 || pend[0].Caller != "scout" || pend[0].Name != "New canvas" {
		t.Fatalf("proposal = %+v", pend)
	}
	// It must not have opened the session.
	if _, ok := st.Get("new-canvas"); ok {
		t.Fatal("propose_session opened a session directly")
	}
	// An agent cannot open a breakout under a missing parent.
	if _, isErr := call(h, t, "propose_session",
		`{"name":"Child","mode":"breakout","parent":"ghost"}`); !isErr {
		t.Fatal("proposed a breakout under a missing parent")
	}
}
