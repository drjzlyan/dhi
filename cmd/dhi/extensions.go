package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/assign"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	agentkitOrg "github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/skillprop"
	"github.com/drjzlyan/dhi/internal/gitcore"
	settingsview "github.com/drjzlyan/dhi/internal/tui/surfaces/settings"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/wizard"
	"github.com/drjzlyan/dhi/internal/version"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Skills, MCP servers and plugins in the standard formats (ADR-0043).

// userExtDir is <config>/dhi: your skills, servers and install record for
// every workspace ("" when there is no config dir).
var userExtDir string

// setupExtensionPaths sets where skills and servers are read from.
func setupExtensionPaths(cfgDir string) {
	userExtDir = cfgDir
	library.UserSkillsDir = filepath.Join(cfgDir, "skills")
	mcpserver.UserFile = filepath.Join(cfgDir, "mcp.json")
	if home, err := os.UserHomeDir(); err == nil {
		library.AgentsSkillsDir = filepath.Join(home, ".agents", "skills")
	}
	if cache, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cache, "dhi", "builtin-skills", version.Version)
		if err := library.MaterializeBuiltins(dir); err == nil {
			library.BuiltinSkillsDir = dir // agents read built-ins like any skill
		}
	}
}

func userScope() install.Scope {
	if userExtDir == "" {
		return install.Scope{}
	}
	return install.Scope{Name: "every workspace", SkillsDir: library.UserSkillsDir,
		MCPFile: mcpserver.UserFile, LockPath: filepath.Join(userExtDir, install.LockFile)}
}

func workspaceScope(root string) install.Scope {
	if root == "" {
		return install.Scope{}
	}
	return install.Scope{Name: "this workspace", SkillsDir: filepath.Join(root, workspace.DirSkills),
		MCPFile: mcpserver.WorkspaceFile(root), LockPath: filepath.Join(root, ".dhi", install.LockFile)}
}

func extCache() string {
	if c, err := os.UserCacheDir(); err == nil {
		return filepath.Join(c, "dhi")
	}
	return os.TempDir()
}

var extHTTP = &http.Client{Timeout: 30 * time.Second}

// newExtensions is the Settings install seam for the workspace at root.
func newExtensions(root string) *settingsview.Extensions {
	return &settingsview.Extensions{
		Fetch: gitcore.CloneAt, HTTP: extHTTP, Tmp: os.TempDir(), Cache: extCache(),
		Workspace: workspaceScope(root), User: userScope(),
	}
}

// allBots is the runtime's "every bot" policy for the workspace at root.
func allBots(root string) func() assign.Policy {
	return func() assign.Policy {
		var locks []*install.Lock
		for _, sc := range []install.Scope{workspaceScope(root), userScope()} {
			if sc.LockPath == "" {
				continue
			}
			if l, err := install.ReadLock(sc.LockPath); err == nil {
				locks = append(locks, l)
			}
		}
		return assign.PolicyFrom(locks...)
	}
}

// officialMarketplace fetches the vendors' plugin catalog (cached a day).
func officialMarketplace(ctx context.Context) (*install.Marketplace, error) {
	src, err := install.ParseSource(install.DefaultMarketplace)
	if err != nil {
		return nil, err
	}
	m, _, err := install.FetchMarketplace(ctx, src, extHTTP, gitcore.CloneAt, extCache(), false)
	return m, err
}

// official are the vendors' own plugins setup offers (names in
// anthropics/claude-plugins-official, published by each vendor).
var official = []wizard.IntegrationRow{
	{Name: "Atlassian", Plugin: "atlassian", Description: "Jira and Confluence: search and create issues, read and write pages, plan sprints.",
		SignIn: "Sign in with your Atlassian account when a bot first uses it."},
	{Name: "Slack", Plugin: "slack", Description: "Search messages, read channels and threads, post updates.",
		SignIn: "Sign in with your Slack account when a bot first uses it."},
	{Name: "Zoom", Plugin: "zoom-plugin", Description: "Meetings, recordings, chat, docs and whiteboards.",
		SignIn: "Zoom's plugin asks for an access token: DHI asks for it, masked, when a bot first uses it."},
	{Name: "Linear", Plugin: "linear", Description: "Issues, projects and cycles.",
		SignIn: "Sign in with your Linear account when a bot first uses it."},
	{Name: "Notion", Plugin: "notion", Description: "Search and edit pages and databases.",
		SignIn: "Sign in with your Notion account when a bot first uses it."},
	{Name: "GitHub", Plugin: "github", Description: "Issues, pull requests and code search.",
		SignIn: "Sign in with GitHub when a bot first uses it."},
}

// integrationRows marks which official plugins are already installed.
func integrationRows(root string) []wizard.IntegrationRow {
	have := map[string]bool{}
	for _, sc := range []install.Scope{workspaceScope(root), userScope()} {
		if l, err := install.ReadLock(sc.LockPath); err == nil {
			for _, r := range l.Servers {
				have[r.Plugin] = true
			}
		}
	}
	out := append([]wizard.IntegrationRow(nil), official...)
	for i := range out {
		out[i].Installed = have[out[i].Plugin]
	}
	return out
}

// setupIntegration installs a vendor plugin into the workspace and gives
// its skills and servers to who.
func setupIntegration(ctx context.Context, root, plugin, who string) (string, error) {
	m, err := officialMarketplace(ctx)
	if err != nil {
		return "", err
	}
	entry, ok := m.Find(plugin)
	if !ok {
		return "", fmt.Errorf("%s is not in the official marketplace", plugin)
	}
	b, err := install.Preview(ctx, entry.Source, gitcore.CloneAt, os.TempDir())
	if err != nil {
		return "", err
	}
	defer b.Close()
	sc := workspaceScope(root)
	res, err := install.Apply(b, sc, nil, false)
	if err != nil {
		return "", err
	}
	items := make([]assign.Item, 0, len(res.Skills)+len(res.Servers))
	for _, c := range b.Skills {
		items = append(items, assign.Item{Kind: assign.KindSkill, Name: c.Skill.Slug, Description: c.Skill.Description})
	}
	for _, n := range res.Servers {
		items = append(items, assign.Item{Kind: assign.KindServer, Name: n, Description: entry.Description, Category: entry.Category})
	}
	return giveTo(root, sc, items, who)
}

// giveTo assigns items per the setup choice and names the outcome.
func giveTo(root string, sc install.Scope, items []assign.Item, who string) (string, error) {
	ws, err := workspace.Load(root)
	if err != nil {
		return "", err
	}
	o, err := agentkitOrg.Load(root)
	if err != nil {
		return "", err
	}
	roster, err := agentkitOrg.LoadRoster(ws)
	if err != nil {
		return "", err
	}
	switch who {
	case "every bot":
		lock, err := install.ReadLock(sc.LockPath)
		if err != nil {
			return "", err
		}
		for _, it := range items {
			lock.SetAllBots(it.Kind, it.Name, true)
		}
		return "every bot has it", lock.Write(sc.LockPath)
	case "nobody yet":
		return "no bot has it yet (Settings → INTEGRATIONS → g)", nil
	}
	lib := library.Open(ws)
	got := map[string]bool{}
	for _, it := range items {
		var ids []string
		for _, p := range assign.Recommend(it, roster, lib, o) {
			ids = append(ids, p.AgentID)
			got[p.AgentID] = true
		}
		if _, _, err := assign.Apply(ws, o, it, ids); err != nil {
			return "", err
		}
	}
	if len(got) == 0 {
		return "no bot fits it yet (Settings → INTEGRATIONS → g)", nil
	}
	var names []string
	for id := range got {
		names = append(names, id)
	}
	return strings.Join(sortStrings(names), ", ") + " have it", nil
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

// skillSearch lists the official marketplace's plugins matching q.
func skillSearch(q string) ([]dhitools.SkillHit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m, err := officialMarketplace(ctx)
	if err != nil {
		return nil, err
	}
	var out []dhitools.SkillHit
	for _, p := range m.Search(q) {
		out = append(out, dhitools.SkillHit{Name: p.Name, Description: p.Description})
	}
	return out, nil
}

// skillSuggest records an agent's suggestion for the human (F-091):
// entry is a marketplace plugin name or a GitHub/git URL.
func skillSuggest(root string) func(ctx context.Context, agent, entry, reason string) (string, error) {
	return func(ctx context.Context, agent, entry, reason string) (string, error) {
		src, err := install.ParseSource(entry)
		if err != nil || src.Path != "" { // agents suggest published sources only
			m, merr := officialMarketplace(ctx)
			if merr != nil {
				return "", merr
			}
			p, ok := m.Find(entry)
			if !ok {
				return "", fmt.Errorf("%q is neither a plugin in the marketplace nor a GitHub/git URL", entry)
			}
			src = p.Source
		}
		p, err := skillprop.Review(ctx, gitcore.CloneAt, os.TempDir(), src)
		if err != nil {
			return "", err
		}
		p.Agent, p.Reason = agent, reason
		if _, err := skillprop.Open(root).Propose(p); err != nil {
			return "", err
		}
		what := fmt.Sprintf("%d skill(s), %s", len(p.Skills), p.Trust())
		if p.HasScripts() {
			what += ", ships a script"
		}
		return what, nil
	}
}
