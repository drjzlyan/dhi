package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The vendors' own plugin configs (testdata, verbatim from their repos)
// parse as published.
func TestVendorConfigsParse(t *testing.T) {
	read := func(name string) []Server {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		servers, warns, err := Parse(data, "plugin", name)
		if err != nil || len(warns) > 0 {
			t.Fatalf("%s: %v %v", name, err, warns)
		}
		return servers
	}
	slack := read("slack.mcp.json")
	if len(slack) != 1 || slack[0].Transport != HTTP || slack[0].URL != "https://mcp.slack.com/mcp" ||
		slack[0].OAuth == nil || slack[0].OAuth.ClientID == "" || slack[0].OAuth.CallbackPort != 3118 {
		t.Fatalf("slack: %+v", slack)
	}
	atl := read("atlassian.mcp.json")
	if len(atl) != 1 || atl[0].URL != "https://mcp.atlassian.com/v2/mcp" || atl[0].OAuth != nil {
		t.Fatalf("atlassian: %+v", atl)
	}
	zoom := read("zoom.mcp.json")
	if len(zoom) != 7 {
		t.Fatalf("zoom servers = %d", len(zoom))
	}
	if v := zoom[0].Vars(); len(v) != 1 || !strings.HasPrefix(v[0], "ZOOM_") {
		t.Fatalf("zoom vars = %v", v)
	}
}

func TestExpand(t *testing.T) {
	look := func(n string) (string, bool) { return map[string]string{"TOKEN": "t0k"}[n], n == "TOKEN" }
	if v, err := Expand("Bearer ${TOKEN}", look); err != nil || v != "Bearer t0k" {
		t.Fatalf("%q %v", v, err)
	}
	if v, err := Expand("${MISSING:-dflt}", look); err != nil || v != "dflt" {
		t.Fatalf("default: %q %v", v, err)
	}
	if _, err := Expand("${MISSING}", look); err == nil || !strings.Contains(err.Error(), "MISSING is not set") {
		t.Fatalf("missing: %v", err)
	}
}

func TestEntryValidation(t *testing.T) {
	for name, raw := range map[string]string{
		"no command":   `{"type":"stdio"}`,
		"plain http":   `{"type":"http","url":"http://example.com/mcp"}`,
		"legacy sse":   `{"type":"sse","url":"https://x/sse"}`,
		"unknown type": `{"type":"ws","url":"https://x"}`,
	} {
		if _, err := ParseEntry("x", []byte(raw)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if s, err := ParseEntry("loc", []byte(`{"url":"http://127.0.0.1:9/mcp"}`)); err != nil || s.Transport != HTTP {
		t.Fatalf("loopback http without type: %+v %v", s, err)
	}
	if _, err := ParseEntry("bad__name", []byte(`{"command":"x"}`)); err == nil {
		t.Fatal("a name with __ would break mcp__<server>__<tool>")
	}
}

// Add keeps other servers and other keys exactly; Remove drops one.
func TestAddRemovePreserveTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"other":"keep","mcpServers":{"a":{"command":"a-cmd","custom":1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Add(path, "b", []byte(`{"type":"http","url":"https://b.example/mcp"}`)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"other": "keep"`) || !strings.Contains(string(data), `"custom": 1`) {
		t.Fatalf("lost keys:\n%s", data)
	}
	if err := Remove(path, "a"); err != nil {
		t.Fatal(err)
	}
	servers, _, err := Parse(mustRead(t, path), SourceWorkspace, path)
	if err != nil || len(servers) != 1 || servers[0].Slug != "b" {
		t.Fatalf("after remove: %+v %v", servers, err)
	}
}

func TestStorePrecedence(t *testing.T) {
	root := t.TempDir()
	UserFile = filepath.Join(t.TempDir(), "mcp.json")
	t.Cleanup(func() { UserFile = "" })
	if err := Add(UserFile, "jira", []byte(`{"url":"https://user.example/mcp"}`)); err != nil {
		t.Fatal(err)
	}
	if err := Add(WorkspaceFile(root), "jira", []byte(`{"url":"https://ws.example/mcp"}`)); err != nil {
		t.Fatal(err)
	}
	st := Open(root)
	if s, ok := st.Get("jira"); !ok || s.URL != "https://ws.example/mcp" || s.Source != SourceWorkspace {
		t.Fatalf("workspace must win: %+v", s)
	}
}

func TestReadLegacyCards(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, LegacyDir)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "linear.toml"), []byte("schema = 2\nname = \"Linear\"\ntransport = \"http\"\nurl = \"https://mcp.linear.app/mcp\"\norigins = [\"mcp.linear.app\"]\nauth_env = \"LINEAR_API_KEY\"\n"), 0o644)
	got, files, err := ReadLegacy(root)
	if err != nil || len(got) != 1 || len(files) != 1 {
		t.Fatalf("%+v %v %v", got, files, err)
	}
	if got[0].Headers["Authorization"] != "Bearer ${LINEAR_API_KEY}" || got[0].Transport != HTTP {
		t.Fatalf("converted: %+v", got[0])
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
