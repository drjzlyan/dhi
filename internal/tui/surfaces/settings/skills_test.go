package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/registry"
	"github.com/drjzlyan/dhi/internal/agentkit/skillprop"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// F-091: a suggested skill shows in PROPOSALS with its trust; v shows the
// script in full; y installs it where chosen and resolves the suggestion.
func TestSuggestedSkillReviewAndInstall(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	pk := t.TempDir()
	os.MkdirAll(filepath.Join(pk, "skills"), 0o755)
	os.WriteFile(filepath.Join(pk, "skills", "tf.md"),
		[]byte("---\nname: Terraform\ndescription: plan first\nscript: tf.sh\n---\n\nRun plan first.\n"), 0o644)
	os.WriteFile(filepath.Join(pk, "skills", "tf.sh"), []byte("#!/bin/sh\nterraform plan\n"), 0o644)
	os.WriteFile(filepath.Join(pk, "pack.toml"), []byte("schema = 2\nname = \"tf\"\nversion = \"1\"\nskills = [\"skills/tf.md\"]\n"), 0o644)
	sum, _ := registry.Digest(pk)
	idx := t.TempDir()
	os.WriteFile(filepath.Join(idx, "index.toml"), []byte("schema = 1\n\n[[pack]]\nname = \"tf\"\nversion = \"1\"\ndescription = \"d\"\nsource = \""+pk+"\"\nsha256 = \""+sum+"\"\n"), 0o644)
	reg := registry.New(ws)
	if err := reg.Refresh(context.Background(), idx); err != nil {
		t.Fatal(err)
	}
	m.d.Registry = reg
	st := skillprop.Open(ws.Root)
	m.d.SkillProps = st
	p, err := skillprop.Review(context.Background(), reg, "tf")
	if err != nil {
		t.Fatal(err)
	}
	p.Agent, p.Reason = "nobody-here", "infra work"
	if _, err := st.Propose(p); err != nil {
		t.Fatal(err)
	}
	for m.sec != secProposals {
		feed(m, "]")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "skill tf") || !strings.Contains(v, "ships a script") {
		t.Fatalf("PROPOSALS:\n%s", v)
	}
	feed(m, "v")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "terraform plan") || !strings.Contains(v, "digest verified") {
		t.Fatalf("review:\n%s", v)
	}
	feed(m, "esc", "y", "tab", "l", "enter") // this workspace; do not add to the (unknown) agent
	if _, err := os.Stat(filepath.Join(ws.Root, workspace.DirSkills, "tf.md")); err != nil {
		t.Fatalf("not installed: %s", m.flash)
	}
	if len(st.Pending()) != 0 {
		t.Fatal("suggestion still pending")
	}
}
