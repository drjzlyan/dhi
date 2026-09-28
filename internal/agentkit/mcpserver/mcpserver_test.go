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
