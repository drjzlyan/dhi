package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// ADR-0043: old .dhi/mcp/*.toml cards move into .dhi/mcp.json; the
// community Atlassian server becomes Atlassian's official one.
func TestMCPCardsMoveToTheStandardFile(t *testing.T) {
	in := fixture(t)
	dir := filepath.Join(in.WS.Root, mcpserver.LegacyDir)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "atlassian.toml"), []byte("schema = 1\nname = \"Atlassian\"\ntransport = \"stdio\"\ncommand = \"uvx\"\nargs = [\"mcp-atlassian@0.23.1\"]\nenv = [\"JIRA_URL\", \"JIRA_API_TOKEN\"]\norigins = [\"x.atlassian.net\"]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "files.toml"), []byte("schema = 1\nname = \"Files\"\ntransport = \"stdio\"\ncommand = \"files-mcp\"\nenv = [\"FILES_TOKEN\"]\n"), 0o644)
	r, ok := byID(Find(in), "mcp-standard-format")
	if !ok || !r.Safe || !strings.Contains(r.What, "official") {
		t.Fatalf("repair = %+v ok=%v", r, ok)
	}
	if res := Run([]Repair{r}, true); len(res.Failed) > 0 {
		t.Fatal(res.Failed)
	}
	st := mcpserver.Open(in.WS.Root)
	if a, ok := st.Get("atlassian"); !ok || a.URL != "https://mcp.atlassian.com/v2/mcp" {
		t.Fatalf("atlassian = %+v", a)
	}
	if f, ok := st.Get("files"); !ok || f.Command != "files-mcp" || f.Env["FILES_TOKEN"] != "${FILES_TOKEN}" {
		t.Fatalf("files = %+v", f)
	}
	if _, err := os.Stat(filepath.Join(dir, "atlassian.toml")); err == nil {
		t.Fatal("the old card stayed")
	}
	if _, ok := byID(Find(in), "mcp-standard-format"); ok {
		t.Fatal("the repair must not apply twice")
	}
}

func TestOldSkillFilesBecomeSkillFolders(t *testing.T) {
	in := fixture(t)
	dir := filepath.Join(in.WS.Root, workspace.DirSkills)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "deploy_checks.md"), []byte("---\nname: Deploy checks\ndescription: run before a deploy\nscript: deploy.sh\n---\n\nRun the checks.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "deploy.sh"), []byte("#!/bin/sh\n"), 0o755)
	if err := in.Org.CreateAgent(in.WS, &manifest.Agent{ID: "atlas", Name: "Atlas", Model: "m", Skills: []string{"deploy_checks"}}); err != nil {
		t.Fatal(err)
	}
	r, ok := byID(Find(in), "skills-standard-format")
	if !ok {
		t.Fatal("no repair")
	}
	if res := Run([]Repair{r}, true); len(res.Failed) > 0 {
		t.Fatal(res.Failed)
	}
	k, ok := library.Open(in.WS).Skill("deploy-checks")
	if !ok || k.Description != "run before a deploy" || !strings.Contains(k.Body, "scripts/deploy.sh") {
		t.Fatalf("skill = %+v ok=%v", k, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "deploy-checks", "scripts", "deploy.sh")); err != nil {
		t.Fatal("the script did not move into the skill")
	}
	if _, err := os.Stat(filepath.Join(dir, "deploy_checks.md")); err == nil {
		t.Fatal("the old file stayed")
	}
	roster, _ := org.LoadRoster(in.WS)
	if len(roster) != 1 || len(roster[0].Skills) != 1 || roster[0].Skills[0] != "deploy-checks" {
		t.Fatalf("the agent must follow the rename: %+v", roster)
	}
}
