package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/agentkit/skillprop"
	"github.com/drjzlyan/dhi/internal/ansi"
)

// F-091 (ADR-0043): a suggested skill shows in PROPOSALS with how it is
// pinned; v shows its script in full; y installs that commit where chosen
// and resolves the suggestion.
func TestSuggestedSkillReviewAndInstall(t *testing.T) {
	m, ws := extSurface(t)
	repo := t.TempDir()
	d := filepath.Join(repo, "tf")
	os.MkdirAll(filepath.Join(d, "scripts"), 0o755)
	os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: tf\ndescription: plan first. Use for terraform.\n---\n\nRun scripts/plan.sh first.\n"), 0o644)
	os.WriteFile(filepath.Join(d, "scripts", "plan.sh"), []byte("#!/bin/sh\nterraform plan\n"), 0o755)
	m.d.Ext.Fetch = func(ctx context.Context, url, ref, dst string) (string, error) {
		return "c0ffee1234567", os.CopyFS(dst, os.DirFS(repo))
	}
	st := skillprop.Open(ws.Root)
	m.d.SkillProps = st
	p, err := skillprop.Review(context.Background(), m.d.Ext.Fetch, t.TempDir(), install.Source{URL: "https://github.com/acme/infra-skills.git"})
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
	if v := ansi.Strip(m.View()); !strings.Contains(v, "infra-skills") || !strings.Contains(v, "ships a script") || !strings.Contains(v, "pinned") {
		t.Fatalf("PROPOSALS:\n%s", v)
	}
	feed(m, "v")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "terraform plan") || !strings.Contains(v, "c0ffee123456") {
		t.Fatalf("review:\n%s", v)
	}
	feed(m, "esc", "y", "tab", "l", "enter") // this workspace; do not add to the (unknown) agent
	if _, err := os.Stat(filepath.Join(m.d.Ext.Workspace.SkillsDir, "tf", "SKILL.md")); err != nil {
		t.Fatalf("not installed: %s", m.flash)
	}
	if len(st.Pending()) != 0 {
		t.Fatal("suggestion still pending")
	}
}
