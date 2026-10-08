// Package catalog is DHI's curated list of integration servers (F-047,
// ADR-0028): which MCP server to run for Jira, Slack, GitHub…, with the
// pinned command, the credentials it needs and where to create them.
//
// An entry exists only when its command, package and environment names come
// from the vendor's or maintainer's own documentation and its version is
// pinned from the package registry. What cannot be offered honestly is listed
// as unavailable, with the reason — never faked.
package catalog

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/credstore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Trust says who stands behind the server's code.
type Trust string

const (
	TrustVendorHosted Trust = "vendor-hosted" // run by the vendor; we only send requests
	TrustVendor       Trust = "vendor"        // vendor-published package run locally
	TrustCommunity    Trust = "community"     // third-party package run locally with your token
)

// Field is one thing the user types during setup.
type Field struct {
	Key     string
	Label   string
	Help    string
	Example string
	Secret  bool
}

// Entry is one integration.
type Entry struct {
	Slug        string
	Name        string
	Description string
	Trust       Trust
	TrustNote   string
	Source      string // the vendor/maintainer page the facts came from
	TokenURL    string // where the user creates the credential
	Steps       []string
	Verified    string // date the facts were read

	Card   mcpserver.Server
	Inputs []Field
	// Derive maps the user's inputs to the credential values the card
	// declares (by env name). nil = inputs are used verbatim by Key.
	Derive func(in map[string]string) (map[string]string, error)

	// Unavailable, when set, is why this integration cannot be set up yet.
	Unavailable string
}

// Credentials lists the credential names the card needs.
func (e Entry) Credentials() []string {
	out := append([]string(nil), e.Card.Env...)
	if e.Card.AuthEnv != "" {
		out = append(out, e.Card.AuthEnv)
	}
	sort.Strings(out)
	return out
}

const verifiedOn = "2026-10-08"

// Entries lists the catalog in display order.
func Entries() []Entry {
	return []Entry{
		atlassian(), github(), linear(), notion(), slack(), teams(),
	}
}

// Get returns one entry.
func Get(slug string) (Entry, bool) {
	for _, e := range Entries() {
		if e.Slug == slug {
			return e, true
		}
	}
	return Entry{}, false
}

func atlassian() Entry {
	return Entry{
		Slug: "atlassian", Name: "Jira + Confluence",
		Description: "Search and update Jira issues, read and write Confluence pages.",
		Trust:       TrustCommunity,
		TrustNote:   "mcp-atlassian is a community project (sooperset/mcp-atlassian), not Atlassian's. It runs locally and sees your API token.",
		Source:      "https://github.com/sooperset/mcp-atlassian",
		TokenURL:    "https://id.atlassian.com/manage-profile/security/api-tokens",
		Steps: []string{
			"Open the API-token page and create a token (a label like “dhi”).",
			"Your site is the address you use for Jira, e.g. acme.atlassian.net.",
			"Atlassian Cloud only here; Server / Data Center use a personal token — edit the card for that.",
		},
		Verified: verifiedOn,
		Card: mcpserver.Server{
			Slug: "atlassian", Name: "Jira + Confluence", Transport: mcpserver.Stdio,
			Description: "Jira and Confluence via mcp-atlassian (community)",
			Command:     "uvx", Args: []string{"mcp-atlassian@0.23.1"},
			Env: []string{"JIRA_URL", "JIRA_USERNAME", "JIRA_API_TOKEN",
				"CONFLUENCE_URL", "CONFLUENCE_USERNAME", "CONFLUENCE_API_TOKEN"},
			Origins: []string{"atlassian.net", "api.atlassian.com", "id.atlassian.com"},
		},
		Inputs: []Field{
			{Key: "site", Label: "site", Help: "your Atlassian Cloud address", Example: "acme.atlassian.net"},
			{Key: "email", Label: "email", Help: "the account that owns the token", Example: "you@acme.com"},
			{Key: "token", Label: "API token", Secret: true},
		},
		Derive: func(in map[string]string) (map[string]string, error) {
			site, err := normalizeSite(in["site"])
			if err != nil {
				return nil, err
			}
			email := strings.TrimSpace(in["email"])
			if !strings.Contains(email, "@") {
				return nil, fmt.Errorf("email must look like you@acme.com")
			}
			token := strings.TrimSpace(in["token"])
			if token == "" {
				return nil, fmt.Errorf("API token is required")
			}
			return map[string]string{
				"JIRA_URL": site, "JIRA_USERNAME": email, "JIRA_API_TOKEN": token,
				"CONFLUENCE_URL": site + "/wiki", "CONFLUENCE_USERNAME": email, "CONFLUENCE_API_TOKEN": token,
			}, nil
		},
	}
}

// normalizeSite accepts "acme", "acme.atlassian.net" or a full URL and
// returns "https://acme.atlassian.net".
func normalizeSite(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("site is required (e.g. acme.atlassian.net)")
	}
	if !strings.Contains(s, "://") {
		if !strings.Contains(s, ".") {
			s += ".atlassian.net"
		}
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("site %q is not a valid address", raw)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("site must be https")
	}
	if !strings.HasSuffix(u.Host, ".atlassian.net") {
		return "", fmt.Errorf("%s is not an Atlassian Cloud site (*.atlassian.net)", u.Host)
	}
	return "https://" + u.Host, nil
}

func github() Entry {
	return Entry{
		Slug: "github", Name: "GitHub",
		Description: "Issues, pull requests, code search and repositories on GitHub.",
		Trust:       TrustVendorHosted,
		TrustNote:   "GitHub runs this server; DHI only sends your requests to api.githubcopilot.com.",
		Source:      "https://github.com/github/github-mcp-server",
		TokenURL:    "https://github.com/settings/personal-access-tokens/new",
		Steps: []string{
			"Create a fine-grained personal access token.",
			"Give it only the repositories and permissions your employees need.",
		},
		Verified: verifiedOn,
		Card: mcpserver.Server{
			Slug: "github", Name: "GitHub", Transport: mcpserver.HTTP,
			Description: "GitHub's hosted MCP server",
			URL:         "https://api.githubcopilot.com/mcp/",
			Origins:     []string{"api.githubcopilot.com"}, AuthEnv: "GITHUB_PERSONAL_ACCESS_TOKEN",
		},
		Inputs: []Field{{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Label: "access token", Secret: true}},
	}
}

func linear() Entry {
	return Entry{
		Slug: "linear", Name: "Linear",
		Description: "Linear issues, projects and cycles.",
		Trust:       TrustVendorHosted,
		TrustNote:   "Linear runs this server; DHI only sends your requests to mcp.linear.app.",
		Source:      "https://linear.app/docs/mcp",
		TokenURL:    "https://linear.app/settings/account/security",
		Steps: []string{
			"Create a personal API key (a read-only key works for a look-only employee).",
		},
		Verified: verifiedOn,
		Card: mcpserver.Server{
			Slug: "linear", Name: "Linear", Transport: mcpserver.HTTP,
			Description: "Linear's hosted MCP server",
			URL:         "https://mcp.linear.app/mcp",
			Origins:     []string{"mcp.linear.app"}, AuthEnv: "LINEAR_API_KEY",
		},
		Inputs: []Field{{Key: "LINEAR_API_KEY", Label: "API key", Secret: true}},
	}
}

func notion() Entry {
	return Entry{
		Slug: "notion", Name: "Notion",
		Description: "Search and edit pages and databases in Notion.",
		Trust:       TrustVendor,
		TrustNote:   "Published by Notion (@notionhq/notion-mcp-server); it runs locally with your integration token.",
		Source:      "https://github.com/makenotion/notion-mcp-server",
		TokenURL:    "https://www.notion.so/profile/integrations",
		Steps: []string{
			"Create an internal integration and copy its secret.",
			"Share the pages or databases you want employees to see with that integration.",
		},
		Verified: verifiedOn,
		Card: mcpserver.Server{
			Slug: "notion", Name: "Notion", Transport: mcpserver.Stdio,
			Description: "Notion via its official server",
			Command:     "npx", Args: []string{"-y", "@notionhq/notion-mcp-server@2.5.2"},
			Env: []string{"NOTION_TOKEN"}, Origins: []string{"api.notion.com"},
		},
		Inputs: []Field{{Key: "NOTION_TOKEN", Label: "integration secret", Secret: true}},
	}
}

func slack() Entry {
	return Entry{
		Slug: "slack", Name: "Slack",
		Description: "Read channels and threads, post messages and reactions in Slack.",
		Trust:       TrustCommunity,
		TrustNote:   "Anthropic's reference Slack server is archived; this is a community fork (zencoderai/slack-mcp-server, v0.0.1). It runs locally and sees your bot token.",
		Source:      "https://github.com/zencoderai/slack-mcp-server",
		TokenURL:    "https://api.slack.com/apps",
		Steps: []string{
			"Create a Slack app, add a bot user and install it to your workspace.",
			"Copy the Bot User OAuth Token (starts with xoxb-).",
			"Your Team ID starts with T (Workspace settings → About).",
			"Invite the bot to any private channel it should read.",
		},
		Verified: verifiedOn,
		Card: mcpserver.Server{
			Slug: "slack", Name: "Slack", Transport: mcpserver.Stdio,
			Description: "Slack via a community fork of the reference server",
			Command:     "npx", Args: []string{"-y", "@zencoderai/slack-mcp-server@0.0.1"},
			Env: []string{"SLACK_BOT_TOKEN", "SLACK_TEAM_ID"}, Origins: []string{"slack.com"},
		},
		Inputs: []Field{
			{Key: "SLACK_BOT_TOKEN", Label: "bot token", Help: "starts with xoxb-", Example: "xoxb-…", Secret: true},
			{Key: "SLACK_TEAM_ID", Label: "team ID", Help: "starts with T", Example: "T01234567"},
		},
		Derive: func(in map[string]string) (map[string]string, error) {
			tok, team := strings.TrimSpace(in["SLACK_BOT_TOKEN"]), strings.TrimSpace(in["SLACK_TEAM_ID"])
			if !strings.HasPrefix(tok, "xoxb-") {
				return nil, fmt.Errorf("a bot token starts with xoxb-")
			}
			if !strings.HasPrefix(team, "T") || len(team) < 3 {
				return nil, fmt.Errorf("a team ID starts with T")
			}
			return map[string]string{"SLACK_BOT_TOKEN": tok, "SLACK_TEAM_ID": team}, nil
		},
	}
}

func teams() Entry {
	return Entry{
		Slug: "teams", Name: "Microsoft Teams",
		Description: "Chats, channels and messages in Microsoft Teams.",
		Trust:       TrustVendor,
		Source:      "https://learn.microsoft.com/microsoftteams/",
		Verified:    verifiedOn,
		Unavailable: "Microsoft's Teams MCP server signs in through Entra ID with delegated (interactive) permissions; app-only access cannot post messages. DHI has no OAuth client yet, so it cannot connect — this entry will turn on when it does.",
	}
}

// ---- install ----

// Install writes the card and stores the credentials. Credentials are
// derived from the user's inputs, validated against what the card declares,
// and nothing is left half-done: the card is written first (it validates),
// and removed again if the credentials cannot be stored.
func Install(root string, st *credstore.Store, e Entry, inputs map[string]string) error {
	if e.Unavailable != "" {
		return fmt.Errorf("%s is not available: %s", e.Name, e.Unavailable)
	}
	creds, err := credentials(e, inputs)
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("catalog: no credential store")
	}
	srv := e.Card
	if err := mcpserver.Write(root, &srv); err != nil {
		return err
	}
	if err := st.Set(creds); err != nil {
		_ = mcpserver.Delete(root, e.Slug)
		return err
	}
	return nil
}

func credentials(e Entry, inputs map[string]string) (map[string]string, error) {
	var creds map[string]string
	if e.Derive != nil {
		var err error
		if creds, err = e.Derive(inputs); err != nil {
			return nil, err
		}
	} else {
		creds = map[string]string{}
		for k, v := range inputs {
			creds[k] = strings.TrimSpace(v)
		}
	}
	for _, name := range e.Credentials() {
		if strings.TrimSpace(creds[name]) == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}
	// Store exactly the declared names — nothing extra the form happened to hold.
	out := map[string]string{}
	for _, name := range e.Credentials() {
		out[name] = creds[name]
	}
	return out, nil
}

// State is an entry's setup state on this machine.
type State struct {
	Installed bool     // the card exists in the workspace
	Missing   []string // declared credentials that do not resolve
}

// Ready reports installed with every credential resolvable.
func (s State) Ready() bool { return s.Installed && len(s.Missing) == 0 }

// Status reports whether an entry is set up, resolving credentials with
// lookup (the bridge's DefaultLookup in production).
func Status(root string, e Entry, lookup func(string) (string, bool)) State {
	var s State
	if e.Unavailable != "" {
		return s
	}
	_, err := os.Stat(mcpserver.Path(root, e.Slug))
	s.Installed = err == nil
	for _, name := range e.Credentials() {
		if v, ok := lookup(name); !ok || v == "" {
			s.Missing = append(s.Missing, name)
		}
	}
	return s
}

// ToolRef is the manifest tool reference that allows every tool of an entry.
func (e Entry) ToolRef() string { return "mcp__" + e.Slug + "__*" }

// Enable adds the entry's tool reference to the given employees. Every
// resulting manifest is validated before the first is written; agents that
// already have it are left untouched; everything else in a manifest is kept.
func Enable(ws *workspace.Workspace, o *org.Org, e Entry, agentIDs []string) ([]string, error) {
	roster, err := org.LoadRoster(ws)
	if err != nil {
		return nil, err
	}
	byID := map[string]*manifest.Agent{}
	for _, a := range roster {
		byID[a.ID] = a
	}
	var changed []*manifest.Agent
	for _, id := range agentIDs {
		a, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown employee %q", id)
		}
		has := false
		for _, t := range a.Tools {
			if t == e.ToolRef() {
				has = true
			}
		}
		if has {
			continue
		}
		cp := *a
		cp.Tools = append(append([]string(nil), a.Tools...), e.ToolRef())
		if _, err := manifest.Marshal(&cp); err != nil {
			return nil, err
		}
		changed = append(changed, &cp)
	}
	var ids []string
	for _, a := range changed {
		if err := o.UpdateAgent(ws, a); err != nil {
			return ids, err
		}
		ids = append(ids, a.ID)
	}
	return ids, nil
}
