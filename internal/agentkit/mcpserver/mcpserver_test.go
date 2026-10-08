package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStdioAndHTTP(t *testing.T) {
	s, err := Parse("fs", []byte("schema = 1\nname = \"Filesystem\"\ntransport = \"stdio\"\ncommand = \"npx\"\nargs = [\"-y\", \"srv\"]\norigins = [\"api.example.com\", \"localhost:8080\"]\nenv = [\"FS_TOKEN\"]\n"))
	if err != nil {
		t.Fatalf("stdio: %v", err)
	}
	if s.Transport != Stdio || s.Command != "npx" || len(s.Args) != 2 || len(s.Origins) != 2 || len(s.Env) != 1 {
		t.Fatalf("stdio server = %+v", s)
	}
	h, err := Parse("web", []byte("schema = 1\nname = \"Web\"\ntransport = \"http\"\nurl = \"https://mcp.example.com/mcp\"\n"))
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	if h.Transport != HTTP || h.URL == "" {
		t.Fatalf("http server = %+v", h)
	}
	if _, err := Parse("loop", []byte("schema = 1\nname = \"Loop\"\ntransport = \"http\"\nurl = \"http://127.0.0.1:9000/mcp\"\n")); err != nil {
		t.Fatalf("loopback http: %v", err)
	}
}

func TestParseRefusals(t *testing.T) {
	cases := map[string]string{
		"stdio no command": "schema = 1\nname = \"X\"\ntransport = \"stdio\"\n",
		"http no url":      "schema = 1\nname = \"X\"\ntransport = \"http\"\n",
		"http plain":       "schema = 1\nname = \"X\"\ntransport = \"http\"\nurl = \"http://evil.example.com\"\n",
		"stdio has url":    "schema = 1\nname = \"X\"\ntransport = \"stdio\"\ncommand = \"x\"\nurl = \"https://a\"\n",
		"bad transport":    "schema = 1\nname = \"X\"\ntransport = \"carrier-pigeon\"\n",
		"no name":          "schema = 1\ntransport = \"stdio\"\ncommand = \"x\"\n",
		"env value":        "schema = 1\nname = \"X\"\ntransport = \"stdio\"\ncommand = \"x\"\nenv = [\"sk-123\"]\n",
		"bad origin":       "schema = 1\nname = \"X\"\ntransport = \"stdio\"\ncommand = \"x\"\norigins = [\"bad origin\"]\n",
		"unknown key":      "schema = 1\nname = \"X\"\ntransport = \"stdio\"\ncommand = \"x\"\nsecret = \"oops\"\n",
		"bad schema":       "schema = 7\nname = \"X\"\ntransport = \"stdio\"\ncommand = \"x\"\n",
	}
	for name, body := range cases {
		if _, err := Parse("s", []byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStoreRoundTripAndWarnings(t *testing.T) {
	root := t.TempDir()
	srv := &Server{Slug: "fs", Name: "Filesystem", Transport: Stdio, Command: "npx",
		Args: []string{"-y", "srv"}, Env: []string{"FS_TOKEN"}, Origins: []string{"api.example.com"}}
	if err := Write(root, srv); err != nil {
		t.Fatalf("Write: %v", err)
	}
	st := Open(root)
	if got, ok := st.Get("fs"); !ok || got.Name != "Filesystem" || got.Transport != Stdio {
		t.Fatalf("Get = %+v ok=%v", got, ok)
	}
	if len(st.Servers()) != 1 || len(st.Warnings()) != 0 {
		t.Fatalf("store = %d servers, warnings %v", len(st.Servers()), st.Warnings())
	}
	// Malformed card is a warning, not a failure.
	os.WriteFile(filepath.Join(root, Dir, "broken.toml"), []byte("schema = 1\n"), 0o644)
	st = Open(root)
	if len(st.Warnings()) != 1 || !strings.Contains(st.Warnings()[0], "broken") {
		t.Fatalf("warnings = %v", st.Warnings())
	}
	if err := Delete(root, "fs"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := Open(root).Get("fs"); ok {
		t.Fatal("fs survived Delete")
	}
	// Deleting a missing server is not an error.
	if err := Delete(root, "fs"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestAuthEnvRulesAndSchemaCompat(t *testing.T) {
	ok := "schema = 2\nname = \"Linear\"\ntransport = \"http\"\nurl = \"https://mcp.linear.app/mcp\"\norigins = [\"mcp.linear.app\"]\nauth_env = \"LINEAR_API_KEY\"\n"
	s, err := Parse("linear", []byte(ok))
	if err != nil || s.AuthEnv != "LINEAR_API_KEY" {
		t.Fatalf("auth_env card = %+v err=%v", s, err)
	}
	bad := map[string]string{
		"schema 1 + auth_env": strings.Replace(ok, "schema = 2", "schema = 1", 1),
		"stdio + auth_env":    "schema = 2\nname = \"x\"\ntransport = \"stdio\"\ncommand = \"c\"\nauth_env = \"TOK\"\n",
		"bad name":            strings.Replace(ok, "LINEAR_API_KEY", "not a name", 1),
		"a value, not a name": strings.Replace(ok, "LINEAR_API_KEY", "ghp_abc123=", 1),
		"loopback http":       "schema = 2\nname = \"x\"\ntransport = \"http\"\nurl = \"http://127.0.0.1:9\"\nauth_env = \"TOK\"\n",
		"schema 3":            strings.Replace(ok, "schema = 2", "schema = 3", 1),
	}
	for name, doc := range bad {
		if _, err := Parse("x", []byte(doc)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// Existing schema-1 cards keep loading.
	old := "schema = 1\nname = \"Files\"\ntransport = \"stdio\"\ncommand = \"fs-server\"\n"
	if _, err := Parse("files", []byte(old)); err != nil {
		t.Fatalf("schema 1 card no longer loads: %v", err)
	}
}

func TestAuthEnvRoundTripsThroughWrite(t *testing.T) {
	root := t.TempDir()
	in := &Server{Slug: "linear", Name: "Linear", Transport: HTTP, URL: "https://mcp.linear.app/mcp",
		Origins: []string{"mcp.linear.app"}, AuthEnv: "LINEAR_API_KEY"}
	if err := Write(root, in); err != nil {
		t.Fatal(err)
	}
	got, ok := Open(root).Get("linear")
	if !ok || got.AuthEnv != "LINEAR_API_KEY" {
		t.Fatalf("round trip = %+v ok=%v", got, ok)
	}
	data, _ := os.ReadFile(Path(root, "linear"))
	if !strings.Contains(string(data), "schema = 2") || strings.Contains(string(data), "Bearer") {
		t.Fatalf("card:\n%s", data)
	}
}
