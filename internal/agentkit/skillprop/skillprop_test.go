package skillprop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/registry"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func newWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "main"), 0o755)
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// skillRegistry publishes a one-skill pack (with a script) in a local
// registry the workspace has refreshed.
func skillRegistry(t *testing.T, ws *workspace.Workspace) (*registry.Registry, string) {
	t.Helper()
	pk := t.TempDir()
	os.MkdirAll(filepath.Join(pk, "skills"), 0o755)
	os.WriteFile(filepath.Join(pk, "skills", "tf.md"),
		[]byte("---\nname: Terraform\ndescription: plan before apply\nscript: tf.sh\n---\n\nAlways run terraform plan first.\n"), 0o644)
	os.WriteFile(filepath.Join(pk, "skills", "tf.sh"), []byte("#!/bin/sh\nterraform plan\n"), 0o644)
	os.WriteFile(filepath.Join(pk, "pack.toml"), []byte("schema = 2\nname = \"tf\"\nversion = \"1\"\nskills = [\"skills/tf.md\"]\n"), 0o644)
	sum, err := registry.Digest(pk)
	if err != nil {
		t.Fatal(err)
	}
	idx := t.TempDir()
	os.WriteFile(filepath.Join(idx, "index.toml"), []byte("schema = 1\n\n[[pack]]\nname = \"tf\"\nversion = \"1\"\ndescription = \"terraform skill\"\nsource = \""+pk+"\"\nsha256 = \""+sum+"\"\n"), 0o644)
	r := registry.New(ws)
	if err := r.Refresh(context.Background(), idx); err != nil {
		t.Fatal(err)
	}
	return r, pk
}

// F-091: a suggestion records every skill's text and script in full; the
// approval installs exactly that, here or for every workspace.
func TestReviewThenInstallHereAndEverywhere(t *testing.T) {
	ws := newWS(t)
	reg, _ := skillRegistry(t, ws)
	p, err := Review(context.Background(), reg, "tf")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Skills) != 1 || !strings.Contains(p.Skills[0].ScriptText, "terraform plan") || !p.HasScripts() || p.Signed {
		t.Fatalf("review = %+v", p)
	}
	st := Open(ws.Root)
	p.Agent, p.Reason = "forge", "infra work"
	p, _ = st.Propose(p)
	if len(st.Pending()) != 1 {
		t.Fatal("not pending")
	}
	slugs, err := Install(context.Background(), reg, ws, p, ScopeWorkspace)
	if err != nil || strings.Join(slugs, ",") != "tf" {
		t.Fatalf("install %v %v", slugs, err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, workspace.DirSkills, "tf.sh")); err != nil {
		t.Fatal("script not installed beside the skill")
	}
	library.UserSkillsDir = filepath.Join(t.TempDir(), "skills")
	t.Cleanup(func() { library.UserSkillsDir = "" })
	if _, err := Install(context.Background(), reg, ws, p, ScopeEverywhere); err != nil {
		t.Fatal(err)
	}
	other := library.Open(newWS(t))
	if _, ok := other.Skill("tf"); !ok || other.Source("skill", "tf") != library.SourceYours {
		t.Fatal("a skill installed for every workspace is missing from another one")
	}
}

// Content that changed after review is refused at install.
func TestInstallRefusesWhatWasNotReviewed(t *testing.T) {
	ws := newWS(t)
	reg, _ := skillRegistry(t, ws)
	p, err := Review(context.Background(), reg, "tf")
	if err != nil {
		t.Fatal(err)
	}
	p.SHA256 = strings.Repeat("0", 64)
	if _, err := Install(context.Background(), reg, ws, p, ScopeWorkspace); err == nil || !strings.Contains(err.Error(), "changed since it was reviewed") {
		t.Fatalf("err = %v", err)
	}
}

// A pack without skills is not a skill suggestion.
func TestReviewRefusesAPackWithoutSkills(t *testing.T) {
	ws := newWS(t)
	pk := t.TempDir()
	os.WriteFile(filepath.Join(pk, "a.toml"), []byte("schema = 1\nname = \"A\"\nmodel = \"m\"\n"), 0o644)
	os.WriteFile(filepath.Join(pk, "pack.toml"), []byte("schema = 2\nname = \"crew\"\nversion = \"1\"\nagents = [\"a.toml\"]\n"), 0o644)
	sum, _ := registry.Digest(pk)
	idx := t.TempDir()
	os.WriteFile(filepath.Join(idx, "index.toml"), []byte("schema = 1\n\n[[pack]]\nname = \"crew\"\nversion = \"1\"\ndescription = \"x\"\nsource = \""+pk+"\"\nsha256 = \""+sum+"\"\n"), 0o644)
	reg := registry.New(ws)
	if err := reg.Refresh(context.Background(), idx); err != nil {
		t.Fatal(err)
	}
	if _, err := Review(context.Background(), reg, "crew"); err == nil || !strings.Contains(err.Error(), "ships no skills") {
		t.Fatalf("err = %v", err)
	}
}
