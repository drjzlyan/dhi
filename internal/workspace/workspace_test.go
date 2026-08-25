package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupWorkspace(t *testing.T) (*Workspace, string) {
	t.Helper()
	root := t.TempDir()
	repoA := filepath.Join(root, "repos", "alpha")
	repoB := filepath.Join(root, "elsewhere", "beta")
	for _, dir := range []string{repoA, repoB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "schema = 1\n\n[members.alpha]\npath = \"repos/alpha\"\n\n[members.beta]\npath = \"" + repoB + "\"\n"
	cfgPath := filepath.Join(root, ConfigFile)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return ws, repoA
}

func TestLoadResolvesMembers(t *testing.T) {
	ws, repoA := setupWorkspace(t)
	if ws.Root == "" || len(ws.Members()) != 2 {
		t.Fatalf("ws = %+v", ws)
	}
	m, ok := ws.Member("alpha")
	if !ok || m.Path != filepath.Clean(repoA) {
		t.Errorf("alpha = %+v (want %s)", m, repoA)
	}
	if _, ok := ws.Member("beta"); !ok {
		t.Error("beta missing")
	}
}

func TestCreateAndLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "myrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(root); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, dir := range []string{DirAgents, DirMemory, DirKnowledge, DirChannels, DirTasks} {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() {
			t.Errorf("reserved dir %s not created (%v)", dir, err)
		}
	}
	if _, err := Load(root); err == nil {
		t.Error("empty member list accepted")
	}
	if err := Create(root); err == nil {
		t.Error("Create overwrote existing config")
	}
}

func TestLoadErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "not a DHI workspace") {
		t.Errorf("missing config: %v", err)
	}

	write := func(content string) {
		t.Helper()
		cfgPath := filepath.Join(root, ConfigFile)
		os.MkdirAll(filepath.Dir(cfgPath), 0o755)
		if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(`schema = 99`)
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Errorf("bad schema: %v", err)
	}
	write("schema = 1\n\n[members.Bad_Name]\npath = \"x\"\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "bad member name") {
		t.Errorf("bad name: %v", err)
	}
	write("schema = 1\n\n[members.ok]\npath = \"missing-dir\"\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("missing member dir: %v", err)
	}
	write("not toml at all ]]")
	if _, err := Load(root); err == nil {
		t.Error("garbage toml accepted")
	}
}

func TestVPathRoundTrip(t *testing.T) {
	ws, repoA := setupWorkspace(t)

	vp, err := ParseVPath("alpha/internal/theme/theme.go")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := ws.Resolve(vp)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repoA, "internal", "theme", "theme.go")
	if abs != want {
		t.Fatalf("Resolve = %q, want %q", abs, want)
	}
	back, err := ws.VPathFor(abs)
	if err != nil {
		t.Fatal(err)
	}
	if back.String() != vp.String() {
		t.Errorf("round trip = %q, want %q", back.String(), vp.String())
	}
}

func TestVPathMemberRoot(t *testing.T) {
	ws, repoA := setupWorkspace(t)
	vp, err := ParseVPath("@alpha/")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := ws.Resolve(vp)
	if err != nil || abs != filepath.Clean(repoA) {
		t.Fatalf("Resolve root = %q, %v", abs, err)
	}
	back, err := ws.VPathFor(abs)
	if err != nil || back.Member != "alpha" || back.Rel != "" {
		t.Errorf("VPathFor root = %+v, %v", back, err)
	}
}

func TestVPathRejectsEscapeAttempts(t *testing.T) {
	ws, _ := setupWorkspace(t)
	for _, bad := range []string{"../outside/x", "alpha/../../../etc/passwd", "", "/"} {
		if _, err := ParseVPath(bad); err == nil {
			t.Errorf("ParseVPath(%q) accepted", bad)
		}
	}
	outside := VPath{Member: "alpha", Rel: "../../escape"}
	if _, err := ws.Resolve(outside); err == nil {
		t.Error("Resolve accepted traversal outside member")
	}
	if _, err := ws.Resolve(VPath{Member: "ghost", Rel: "x"}); err == nil {
		t.Error("unknown member resolved")
	}
}

func TestVPathForOutsideMembersFails(t *testing.T) {
	ws, _ := setupWorkspace(t)
	if _, err := ws.VPathFor(filepath.Join(ws.Root, ".dhi", "memory")); err == nil {
		t.Error(".dhi path mapped to a member")
	}
	if _, err := ws.VPathFor(t.TempDir()); err == nil {
		t.Error("foreign path mapped to a member")
	}
}
