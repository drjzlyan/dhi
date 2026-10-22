package install

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
)

func TestParseSource(t *testing.T) {
	cases := map[string]Source{
		"anthropics/skills":      {URL: "https://github.com/anthropics/skills.git"},
		"github.com/o/r":         {URL: "https://github.com/o/r.git"},
		"https://github.com/o/r": {URL: "https://github.com/o/r.git"},
		"https://github.com/anthropics/skills/tree/main/skills/pdf": {URL: "https://github.com/anthropics/skills.git", Ref: "main", Subdir: "skills/pdf"},
		"https://github.com/o/r/blob/v2/skills/x/SKILL.md":          {URL: "https://github.com/o/r.git", Ref: "v2", Subdir: "skills/x"},
		"https://git.example.com/team/skills.git#main:tools/lint":   {URL: "https://git.example.com/team/skills.git", Ref: "main", Subdir: "tools/lint"},
		"git@github.com:o/r.git":                                    {URL: "git@github.com:o/r.git"},
	}
	for in, want := range cases {
		got, err := ParseSource(in)
		if err != nil || got != want {
			t.Errorf("ParseSource(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	dir := t.TempDir()
	if got, err := ParseSource(dir); err != nil || got.Path != dir {
		t.Errorf("local dir: %+v %v", got, err)
	}
	for _, bad := range []string{"", "not a url at all", "ftp://x/y"} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func scope(t *testing.T) Scope {
	root := t.TempDir()
	return Scope{Name: "this workspace", SkillsDir: filepath.Join(root, "skills"),
		MCPFile: filepath.Join(root, "mcp.json"), LockPath: filepath.Join(root, LockFile)}
}

// Atlassian's own plugin (testdata: its real plugin.json and .mcp.json)
// installs its skill and its official remote server.
func TestInstallAPlugin(t *testing.T) {
	src, _ := ParseSource("./testdata/atlassian")
	b, err := Preview(context.Background(), src, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Plugin == nil || b.Plugin.Name != "atlassian" || len(b.Skills) != 1 || len(b.Servers) != 1 {
		t.Fatalf("bundle: plugin=%+v skills=%d servers=%d", b.Plugin, len(b.Skills), len(b.Servers))
	}
	sc := scope(t)
	res, err := Apply(b, sc, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || res.Skills[0] != "triage-issue" || res.Servers[0] != "atlassian" {
		t.Fatalf("result %+v", res)
	}
	servers, _, err := mcpserver.Parse(mustRead(t, sc.MCPFile), mcpserver.SourceWorkspace, sc.MCPFile)
	if err != nil || servers[0].URL != "https://mcp.atlassian.com/v2/mcp" {
		t.Fatalf("server file: %+v %v", servers, err)
	}
	lock, _ := ReadLock(sc.LockPath)
	if lock.Skills["triage-issue"].Plugin != "atlassian" || lock.Servers["atlassian"].Plugin != "atlassian" {
		t.Fatalf("lock: %+v", lock)
	}
}

// A repository of several skills; scripts arrive non-executable until
// the user allows them.
func TestInstallSkillsFromGitWithScriptsGated(t *testing.T) {
	repo := t.TempDir()
	for _, name := range []string{"lint", "deploy"} {
		d := filepath.Join(repo, "skills", name)
		_ = os.MkdirAll(filepath.Join(d, "scripts"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+name+" things\n---\nRun scripts/go.sh\n"), 0o644)
		_ = os.WriteFile(filepath.Join(d, "scripts", "go.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	}
	fetch := func(ctx context.Context, url, ref, dst string) (string, error) {
		if err := replaceDir(repo, dst, true); err != nil {
			return "", err
		}
		return "abc123", nil
	}
	b, err := Preview(context.Background(), Source{URL: "https://example.com/skills.git"}, fetch, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if len(b.Skills) != 2 || len(b.Skills[0].Scripts) != 1 {
		t.Fatalf("candidates: %+v", b.Skills)
	}
	sc := scope(t)
	if _, err := Apply(b, sc, &Pick{Skills: []string{"lint"}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sc.SkillsDir, "deploy")); err == nil {
		t.Fatal("an unpicked skill was installed")
	}
	script := filepath.Join(sc.SkillsDir, "lint", "scripts", "go.sh")
	if fi, _ := os.Stat(script); fi.Mode()&0o111 != 0 {
		t.Fatal("scripts must not be runnable before the user allows them")
	}
	if err := AllowScripts(sc, "lint"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(script); fi.Mode()&0o111 == 0 {
		t.Fatal("allowed scripts must be runnable")
	}
	lock, _ := ReadLock(sc.LockPath)
	if r := lock.Skills["lint"]; r.Commit != "abc123" || !r.Scripts || !strings.HasPrefix(r.Digest, "sha256:") {
		t.Fatalf("lock: %+v", r)
	}
	if err := Remove(sc, "skill", "lint"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sc.SkillsDir, "lint")); err == nil {
		t.Fatal("remove left the skill")
	}
}

func TestMarketplaceSources(t *testing.T) {
	data := []byte(`{"name":"official","plugins":[
	 {"name":"slack","source":{"source":"url","url":"https://github.com/slackapi/slack-mcp-plugin.git","sha":"ad6be88"}},
	 {"name":"github","source":"./external_plugins/github","category":"productivity"},
	 {"name":"x","source":{"source":"git-subdir","url":"https://github.com/o/r.git","path":"plugins/x","ref":"main"}},
	 {"name":"bad","source":{"source":"npm","package":"x"}}]}`)
	m, warns, err := ParseMarketplace(data, Source{URL: "https://github.com/anthropics/claude-plugins-official.git", Ref: "c0ffee"})
	if err != nil || len(m.Plugins) != 3 || len(warns) != 1 {
		t.Fatalf("%+v %v %v", m, warns, err)
	}
	byName := map[string]Source{}
	for _, p := range m.Plugins {
		byName[p.Name] = p.Source
	}
	if s := byName["slack"]; s.URL != "https://github.com/slackapi/slack-mcp-plugin.git" || s.Ref != "ad6be88" {
		t.Fatalf("url source: %+v", s)
	}
	if s := byName["github"]; s.URL != "https://github.com/anthropics/claude-plugins-official.git" || s.Subdir != "external_plugins/github" || s.Ref != "c0ffee" {
		t.Fatalf("relative source: %+v", s)
	}
	if s := byName["x"]; s.Subdir != "plugins/x" || s.Ref != "main" {
		t.Fatalf("git-subdir: %+v", s)
	}
}

func TestParseSnippet(t *testing.T) {
	servers, err := ParseSnippet(`"jira": {"type":"http","url":"https://mcp.atlassian.com/v2/mcp"}`)
	if err != nil || len(servers) != 1 || servers[0].Slug != "jira" {
		t.Fatalf("%+v %v", servers, err)
	}
	if _, err := ParseSnippet(`{"mcpServers":{}}`); err == nil {
		t.Fatal("an empty snippet must be refused")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFetchMarketplaceFromGitHubIsOneFileAndCached(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/anthropics/claude-plugins-official/HEAD/.claude-plugin/marketplace.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name":"official","plugins":[{"name":"slack","description":"Slack workspace integration","source":{"source":"url","url":"https://github.com/slackapi/slack-mcp-plugin.git","sha":"ad6"}}]}`))
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewrite{to: srv.URL, next: http.DefaultTransport}
	src, _ := ParseSource("anthropics/claude-plugins-official")
	cache := t.TempDir()
	for i := 0; i < 2; i++ {
		m, _, err := FetchMarketplace(context.Background(), src, client, nil, cache, false)
		if err != nil || len(m.Search("slack")) != 1 {
			t.Fatalf("fetch %d: %+v %v", i, m, err)
		}
	}
	if calls != 1 {
		t.Fatalf("the catalog must be cached: %d fetches", calls)
	}
}

// rewrite sends every request to the test server, keeping the path.
type rewrite struct {
	to   string
	next http.RoundTripper
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(r.to)
	req.URL.Scheme, req.URL.Host = u.Scheme, u.Host
	return r.next.RoundTrip(req)
}
