package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/assign"
	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// extSurface is a settings surface with installs wired to the workspace.
func extSurface(t *testing.T) (*Model, *workspace.Workspace) {
	t.Helper()
	m, ws, _, _ := agentSurface(t)
	dhi := filepath.Join(ws.Root, ".dhi")
	m.d.Ext = &Extensions{Tmp: t.TempDir(), Cache: t.TempDir(),
		Workspace: install.Scope{Name: scopeHere, SkillsDir: filepath.Join(ws.Root, workspace.DirSkills),
			MCPFile: filepath.Join(ws.Root, mcpserver.File), LockPath: filepath.Join(dhi, install.LockFile)}}
	m.d.Library = nil
	writeRosterAgent(t, ws.Root, "atlas")
	writeRosterAgent(t, ws.Root, "sage")
	setRole(t, ws, "atlas", "lead")
	setRole(t, ws, "sage", "reviewer")
	return m, ws
}

func setRole(t *testing.T, ws *workspace.Workspace, id, role string) {
	t.Helper()
	o, _ := org.Load(ws.Root)
	roster, _ := org.LoadRoster(ws)
	for _, a := range roster {
		if a.ID == id {
			a.Role = role
			if err := o.UpdateAgent(ws, a); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func skillFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "review-kit")
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review-kit\ndescription: Review a pull request diff. Use when reviewing code.\n---\n\nRead the diff, then comment.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "scripts", "lint.sh"), []byte("#!/bin/sh\n"), 0o755)
	return dir
}

// ADR-0043 + F-107: install a skill from a folder → preview names the
// script → install → the Assign dialog preselects the reviewer → enter.
func TestInstallASkillThenGiveItToTheRecommendedBot(t *testing.T) {
	m, ws := extSurface(t)
	for m.sec != secSkills {
		feed(m, "]")
	}
	feed(m, "i")
	if m.dkind != dlgExtSource {
		t.Fatal("i did not open the source dialog")
	}
	m.HandleKey("paste:" + skillFolder(t))
	feed(m, "enter")
	drainDialogEvent(t, m)
	if m.dkind != dlgExtConfirm {
		t.Fatalf("no confirmation (flash %q)", m.flash)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "skill review-kit") || !strings.Contains(v, "runs: scripts/lint.sh") {
		t.Fatalf("the preview must name the skill and its script:\n%s", v)
	}
	feed(m, "enter")
	if m.extState.assign == nil {
		t.Fatalf("no assign dialog after install (flash %q)", m.flash)
	}
	golden.Snapshot(t, "settings_assign", m.View())
	rows := m.extState.assign.rows
	pre := map[string]bool{}
	for _, r := range rows {
		pre[r.id] = r.on
	}
	if !pre["sage"] || pre["atlas"] {
		t.Fatalf("the reviewer should be preselected, not the lead: %+v", rows)
	}
	feed(m, "enter")
	roster, _ := org.LoadRoster(ws)
	if got := assign.Holders(roster, assign.Item{Kind: assign.KindSkill, Name: "review-kit"}); len(got) != 1 || got[0] != "sage" {
		t.Fatalf("holders = %v (flash %q)", got, m.flash)
	}
	k, ok := library.Open(ws).Skill("review-kit")
	if !ok {
		t.Fatal("not installed")
	}
	if fi, _ := os.Stat(filepath.Join(k.Dir, "scripts", "lint.sh")); fi.Mode()&0o111 != 0 {
		t.Fatal("scripts stay off until allowed")
	}
	golden.Snapshot(t, "settings_skills", m.View())
}

// "all bots" is a policy, not a manifest edit: later agents get it too.
func TestAllBotsIsAPolicy(t *testing.T) {
	m, ws := extSurface(t)
	m.openAssign(assign.Item{Kind: assign.KindSkill, Name: "docs", Description: "write docs"}, nil)
	feed(m, "a", "enter")
	lock, _ := install.ReadLock(m.d.Ext.Workspace.LockPath)
	if len(lock.AllBots.Skills) != 1 || lock.AllBots.Skills[0] != "docs" {
		t.Fatalf("policy = %+v", lock.AllBots)
	}
	roster, _ := org.LoadRoster(ws)
	if got := assign.Holders(roster, assign.Item{Kind: assign.KindSkill, Name: "docs"}); len(got) != 0 {
		t.Fatalf("all bots must not be written into manifests: %v", got)
	}
	if !strings.Contains(m.flash, "new ones too") {
		t.Fatalf("flash = %q", m.flash)
	}
}

func TestAddAServerByURLAndRemoveIt(t *testing.T) {
	m, ws := extSurface(t)
	for m.sec != secIntegrations {
		feed(m, "]")
	}
	m.extState.plugins = []install.PluginEntry{{Name: "slack", Description: "Slack workspace integration"}}
	feed(m, "+")
	if m.extState.picker == nil {
		t.Fatal("+ did not open the add picker")
	}
	feed(m, "enter") // first entry: a server by URL
	if m.dkind != dlgServerURL {
		t.Fatalf("dialog = %v", m.dkind)
	}
	m.HandleKey("paste:jira")
	feed(m, "tab", "ctrl+u")
	m.HandleKey("paste:https://mcp.atlassian.com/v2/mcp")
	feed(m, "enter")
	if m.extState.assign == nil {
		t.Fatalf("no assign dialog (err %q)", m.dform.Err)
	}
	feed(m, "esc")
	st := mcpserver.Open(ws.Root)
	if s, ok := st.Get("jira"); !ok || s.URL != "https://mcp.atlassian.com/v2/mcp" {
		t.Fatalf("server = %+v", s)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "jira") || !strings.Contains(v, "mcp.atlassian.com") {
		t.Fatalf("INTEGRATIONS:\n%s", v)
	}
	feed(m, "x", "enter")
	if _, ok := mcpserver.Open(ws.Root).Get("jira"); ok {
		t.Fatal("x did not remove the server")
	}
}
