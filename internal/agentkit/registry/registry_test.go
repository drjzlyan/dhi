package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const scoutDoc = `schema = 1
name = "Scout"
model = "m"
tools = ["read"]
runtime = "claude"
`

// makePackDir writes a valid single-agent pack and returns its dir.
func makePackDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scout.toml"), []byte(scoutDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := "schema = 2\nname = \"" + name + "\"\nversion = \"1.0.0\"\ndescription = \"a pack\"\nagents = [\"scout.toml\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "pack.toml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func makeIndexDir(t *testing.T, packs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	body := "schema = 1\n"
	for name, src := range packs {
		sum, err := Digest(src)
		if err != nil {
			t.Fatal(err)
		}
		body += "\n[[pack]]\n" + "name = \"" + name + "\"\nversion = \"1.0.0\"\ndescription = \"a pack\"\n" +
			"source = \"" + src + "\"\nsha256 = \"" + sum + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "index.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestParseValidation(t *testing.T) {
	good := "schema = 1\n[[pack]]\nname = \"a\"\nsource = \"x\"\nsha256 = \"" + strings.Repeat("a", 64) + "\"\n"
	if _, err := Parse([]byte(good)); err != nil {
		t.Fatalf("good index refused: %v", err)
	}
	bad := map[string]string{
		"bad schema":   "schema = 9\n",
		"short digest": "schema = 1\n[[pack]]\nname = \"a\"\nsource = \"x\"\nsha256 = \"abc\"\n",
		"no source":    "schema = 1\n[[pack]]\nname = \"a\"\nsha256 = \"" + strings.Repeat("a", 64) + "\"\n",
		"unknown key":  "schema = 1\nfuture = true\n",
		"dup":          good + "[[pack]]\nname = \"a\"\nsource = \"y\"\nsha256 = \"" + strings.Repeat("b", 64) + "\"\n",
	}
	for name, body := range bad {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRefreshBrowseSearchOffline(t *testing.T) {
	ws := newWS(t)
	packDir := makePackDir(t, "ux")
	indexDir := makeIndexDir(t, map[string]string{"ux": packDir})

	r := New(ws)
	if _, err := r.Browse(); err == nil {
		t.Fatal("browse before refresh should refuse")
	}
	if err := r.Refresh(context.Background(), indexDir); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	entries, err := r.Browse()
	if err != nil || len(entries) != 1 || entries[0].Name != "ux" {
		t.Fatalf("browse = %+v err=%v", entries, err)
	}
	if got, ok := r.Source(); !ok || got != indexDir {
		t.Fatalf("source = %q ok=%v", got, ok)
	}
	if _, ok := r.FetchedAt(); !ok {
		t.Fatal("fetched_at not recorded")
	}
	if hits, _ := r.Search("x"); len(hits) != 1 {
		t.Fatalf("search missed: %+v", hits)
	}
	// Offline: remove the source; browse still works from cache.
	if err := os.RemoveAll(indexDir); err != nil {
		t.Fatal(err)
	}
	if entries, err := r.Browse(); err != nil || len(entries) != 1 {
		t.Fatalf("offline browse = %+v err=%v", entries, err)
	}
}

func TestInstallVerifiesDigest(t *testing.T) {
	ws := newWS(t)
	packDir := makePackDir(t, "ux")
	indexDir := makeIndexDir(t, map[string]string{"ux": packDir})
	r := New(ws)
	if err := r.Refresh(context.Background(), indexDir); err != nil {
		t.Fatal(err)
	}
	in := &pack.Installer{WS: ws}
	res, err := r.Install(context.Background(), "ux", in)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Pack != "ux" || len(res.Agents) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, workspace.DirAgents, "scout.toml")); err != nil {
		t.Fatalf("agent not installed: %v", err)
	}
}

func TestInstallRefusesDigestMismatch(t *testing.T) {
	ws := newWS(t)
	packDir := makePackDir(t, "ux")
	indexDir := makeIndexDir(t, map[string]string{"ux": packDir})
	r := New(ws)
	if err := r.Refresh(context.Background(), indexDir); err != nil {
		t.Fatal(err)
	}
	// Tamper with the pack after the index pinned its digest.
	if err := os.WriteFile(filepath.Join(packDir, "scout.toml"), []byte("schemaless = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := &pack.Installer{WS: ws}
	_, err := r.Install(context.Background(), "ux", in)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("mismatch = %v", err)
	}
	if names, _ := in.Installed(); len(names) != 0 {
		t.Fatalf("mismatch installed something: %v", names)
	}
	if _, err := r.Install(context.Background(), "ghost", in); err == nil {
		t.Fatal("installed an unknown pack")
	}
}

func TestDigestIgnoresGitAndIsStable(t *testing.T) {
	dir := makePackDir(t, "ux")
	a, err := Digest(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Digest(dir)
	if a != b {
		t.Fatal("digest not stable")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: x"), 0o644)
	c, _ := Digest(dir)
	if a != c {
		t.Fatalf("digest changed with .git: %s vs %s", a, c)
	}
}
