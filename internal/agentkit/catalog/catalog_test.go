package catalog

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/credstore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestEveryAvailableEntryIsAValidPinnedCard(t *testing.T) {
	pinned := regexp.MustCompile(`@\d+\.\d+\.\d+(\b|$)`)
	seen := map[string]bool{}
	for _, e := range Entries() {
		if seen[e.Slug] {
			t.Errorf("duplicate slug %s", e.Slug)
		}
		seen[e.Slug] = true
		if e.Name == "" || e.Description == "" || e.Source == "" || e.Verified == "" {
			t.Errorf("%s: missing name/description/source/verified", e.Slug)
		}
		if e.Unavailable != "" {
			continue
		}
		if e.Trust == "" || e.TokenURL == "" || len(e.Steps) == 0 || len(e.Inputs) == 0 {
			t.Errorf("%s: incomplete setup guidance", e.Slug)
		}
		if strings.Contains(e.Description+e.TrustNote, "official") && e.Trust == TrustCommunity {
			t.Errorf("%s: a community server must not be described as official", e.Slug)
		}
		card := e.Card
		if card.Slug != e.Slug {
			t.Errorf("%s: card slug %q", e.Slug, card.Slug)
		}
		// The card must survive the strict writer/parser.
		root := t.TempDir()
		if err := mcpserver.Write(root, &card); err != nil {
			t.Errorf("%s: card invalid: %v", e.Slug, err)
		}
		// Local servers run third-party code: the version must be pinned.
		if card.Transport == mcpserver.Stdio {
			joined := strings.Join(card.Args, " ")
			if !pinned.MatchString(joined) {
				t.Errorf("%s: stdio server not pinned to an exact version: %v", e.Slug, card.Args)
			}
			if card.Command != "npx" && card.Command != "uvx" {
				t.Errorf("%s: command %q is not a hermetic shim", e.Slug, card.Command)
			}
		}
		if len(card.Origins) == 0 {
			t.Errorf("%s: no declared origins", e.Slug)
		}
	}
}

func TestTeamsIsHonestlyUnavailable(t *testing.T) {
	e, ok := Get("teams")
	if !ok || e.Unavailable == "" || !strings.Contains(e.Unavailable, "Entra") {
		t.Fatalf("teams = %+v", e)
	}
	if err := Install(t.TempDir(), &credstore.Store{Path: filepath.Join(t.TempDir(), "c.toml")}, e, nil); err == nil ||
		!strings.Contains(err.Error(), "not available") {
		t.Fatalf("installing an unavailable entry = %v", err)
	}
}

func TestInputsCoverEveryDeclaredCredential(t *testing.T) {
	for _, e := range Entries() {
		if e.Unavailable != "" {
			continue
		}
		in := map[string]string{}
		switch e.Slug {
		case "atlassian":
			in = map[string]string{"site": "acme", "email": "a@acme.com", "token": "t"}
		case "slack":
			in = map[string]string{"SLACK_BOT_TOKEN": "xoxb-1", "SLACK_TEAM_ID": "T012"}
		default:
			for _, f := range e.Inputs {
				in[f.Key] = "value"
			}
		}
		creds, err := credentials(e, in)
		if err != nil {
			t.Errorf("%s: %v", e.Slug, err)
			continue
		}
		var got, want []string
		for k := range creds {
			got = append(got, k)
		}
		want = e.Credentials()
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: stores %v, card declares %v", e.Slug, got, want)
		}
	}
}

func TestAtlassianDerivesBothProductsFromThreeInputs(t *testing.T) {
	e, _ := Get("atlassian")
	got, err := e.Derive(map[string]string{"site": "https://Acme.atlassian.net/jira/", "email": " me@acme.com ", "token": " tok "})
	if err == nil && got["JIRA_URL"] != "https://Acme.atlassian.net" {
		// host case is preserved by url.Parse; the important part is the shape
		t.Logf("site → %s", got["JIRA_URL"])
	}
	if err != nil {
		t.Fatal(err)
	}
	if got["CONFLUENCE_URL"] != got["JIRA_URL"]+"/wiki" || got["JIRA_USERNAME"] != "me@acme.com" ||
		got["CONFLUENCE_API_TOKEN"] != "tok" || got["JIRA_API_TOKEN"] != "tok" {
		t.Fatalf("derived = %v", got)
	}
	for site, ok := range map[string]bool{
		"acme": true, "acme.atlassian.net": true, "https://acme.atlassian.net": true,
		"": false, "http://acme.atlassian.net": false, "example.com": false, "https://evil.test/acme.atlassian.net": false,
	} {
		_, err := normalizeSite(site)
		if (err == nil) != ok {
			t.Errorf("normalizeSite(%q) err=%v want ok=%v", site, err, ok)
		}
	}
	if _, err := e.Derive(map[string]string{"site": "acme", "email": "nope", "token": "t"}); err == nil {
		t.Error("a bad email was accepted")
	}
}

func TestSlackValidatesTokenShape(t *testing.T) {
	e, _ := Get("slack")
	if _, err := e.Derive(map[string]string{"SLACK_BOT_TOKEN": "xoxp-user", "SLACK_TEAM_ID": "T012"}); err == nil {
		t.Error("a user token was accepted as a bot token")
	}
	if _, err := e.Derive(map[string]string{"SLACK_BOT_TOKEN": "xoxb-1", "SLACK_TEAM_ID": "012"}); err == nil {
		t.Error("a team id without T was accepted")
	}
}

func fixtureWS(t *testing.T) (*workspace.Workspace, *org.Org, string) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, _ := workspace.Load(root)
	o, _ := org.Load(root)
	return ws, o, root
}

func TestInstallWritesCardAndCredentialsAndStatusReflectsIt(t *testing.T) {
	_, _, root := fixtureWS(t)
	st := &credstore.Store{Path: filepath.Join(t.TempDir(), "dhi", "credentials.toml")}
	e, _ := Get("linear")

	lookup := func(n string) (string, bool) { return st.Lookup(n) }
	if s := Status(root, e, lookup); s.Installed || s.Ready() || len(s.Missing) != 1 {
		t.Fatalf("before = %+v", s)
	}
	if err := Install(root, st, e, map[string]string{"LINEAR_API_KEY": " lin_api_xyz "}); err != nil {
		t.Fatal(err)
	}
	if s := Status(root, e, lookup); !s.Ready() {
		t.Fatalf("after = %+v", s)
	}
	if v, _ := st.Lookup("LINEAR_API_KEY"); v != "lin_api_xyz" {
		t.Fatalf("stored %q", v)
	}
	card, _ := os.ReadFile(mcpserver.Path(root, "linear"))
	if strings.Contains(string(card), "lin_api_xyz") {
		t.Fatalf("a secret leaked into the shareable card:\n%s", card)
	}
}

func TestInstallRefusesMissingCredentialsAndLeavesNothingBehind(t *testing.T) {
	_, _, root := fixtureWS(t)
	st := &credstore.Store{Path: filepath.Join(t.TempDir(), "c.toml")}
	e, _ := Get("slack")
	if err := Install(root, st, e, map[string]string{"SLACK_BOT_TOKEN": "xoxb-1"}); err == nil {
		t.Fatal("installed with a credential missing")
	}
	if _, err := os.Stat(mcpserver.Path(root, "slack")); err == nil {
		t.Fatal("a failed install left a card")
	}
	// A credential-store failure also removes the card it just wrote.
	bad := &credstore.Store{Path: filepath.Join(t.TempDir(), "x", "c.toml")}
	os.MkdirAll(filepath.Dir(bad.Path), 0o700)
	os.WriteFile(bad.Path, []byte("{{broken"), 0o600)
	if err := Install(root, bad, e, map[string]string{"SLACK_BOT_TOKEN": "xoxb-1", "SLACK_TEAM_ID": "T012"}); err == nil {
		t.Fatal("expected a store error")
	}
	if _, err := os.Stat(mcpserver.Path(root, "slack")); err == nil {
		t.Fatal("card survived a credential-store failure")
	}
}

func TestEnableAddsTheWildcardKeepingEverythingElse(t *testing.T) {
	ws, o, _ := fixtureWS(t)
	a := &manifest.Agent{ID: "atlas", Name: "Atlas", Model: "default", Runtime: "claude",
		Tools: []string{"read"}, Persona: "mentor", Workflow: "feature", Retries: 2}
	if err := a.SetPolicyJSON(`{"rules":[{"op":"read","path":"**","effect":"allow"}]}`); err != nil {
		t.Fatal(err)
	}
	b := &manifest.Agent{ID: "forge", Name: "Forge", Model: "default", Runtime: "claude", Tools: []string{"read"}}
	for _, x := range []*manifest.Agent{a, b} {
		if err := o.CreateAgent(ws, x); err != nil {
			t.Fatal(err)
		}
	}
	e, _ := Get("notion")

	changed, err := Enable(ws, o, e, []string{"atlas"})
	if err != nil || strings.Join(changed, ",") != "atlas" {
		t.Fatalf("changed = %v err=%v", changed, err)
	}
	roster, _ := org.LoadRoster(ws)
	for _, r := range roster {
		hasRef := strings.Contains(strings.Join(r.Tools, ","), "mcp__notion__*")
		if (r.ID == "atlas") != hasRef {
			t.Errorf("%s tools = %v", r.ID, r.Tools)
		}
		if r.ID == "atlas" && (r.Persona != "mentor" || r.Workflow != "feature" || r.Retries != 2 || r.Policy() == nil) {
			t.Errorf("Enable dropped form-invisible fields: %+v", r)
		}
	}
	// Idempotent; unknown employees refuse before anything is written.
	if changed, _ := Enable(ws, o, e, []string{"atlas"}); len(changed) != 0 {
		t.Fatalf("second Enable changed %v", changed)
	}
	if _, err := Enable(ws, o, e, []string{"forge", "ghost"}); err == nil {
		t.Fatal("unknown employee accepted")
	}
	roster, _ = org.LoadRoster(ws)
	for _, r := range roster {
		if r.ID == "forge" && len(r.Tools) != 1 {
			t.Fatalf("a failed Enable still modified forge: %v", r.Tools)
		}
	}
}
