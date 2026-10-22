package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestGitignoreWithoutGitIsCautious(t *testing.T) {
	in := fixture(t)
	in.Ignored = nil
	os.WriteFile(filepath.Join(in.WS.Root, ".gitignore"), []byte(".dhi/channels/\n"), 0o644)
	if _, ok := byID(Find(in), "gitignore"); ok {
		t.Fatal("wrote .dhi/.gitignore though the root one covers .dhi/")
	}
	os.Remove(filepath.Join(in.WS.Root, ".gitignore"))
	if _, ok := byID(Find(in), "gitignore"); !ok {
		t.Fatal("missing file not written")
	}
	os.WriteFile(filepath.Join(in.WS.Root, ".dhi", ".gitignore"), []byte("channels/\n"), 0o644)
	if _, ok := byID(Find(in), "gitignore"); ok {
		t.Fatal("topped up without git to check")
	}
}

func fixture(t *testing.T) Input {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	o, err := org.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return Input{WS: ws, Org: o, Library: library.Open(ws), Ignored: func(string) bool { return false }}
}

func byID(rs []Repair, id string) (Repair, bool) {
	for _, r := range rs {
		if r.ID == id {
			return r, true
		}
	}
	return Repair{}, false
}

func TestGitignoreWrittenThenTopUpOnly(t *testing.T) {
	in := fixture(t)
	path := filepath.Join(in.WS.Root, ".dhi", ".gitignore")
	r, ok := byID(Find(in), "gitignore")
	if !ok || !r.Safe || !strings.Contains(r.What, "write .dhi/.gitignore") {
		t.Fatalf("missing gitignore: %+v", r)
	}
	if res := Run(Find(in), true); len(res.Done) != 1 {
		t.Fatalf("run %+v", res)
	}
	data, _ := os.ReadFile(path)
	if string(data) != setup.GitignoreBody {
		t.Fatalf("body:\n%s", data)
	}
	if _, ok := byID(Find(in), "gitignore"); ok {
		t.Fatal("complete gitignore still repaired")
	}
	// An older DHI wrote fewer entries: only the new ones are appended.
	os.WriteFile(path, []byte("# mine\nchannels/\nmemory/\n"), 0o644)
	r, _ = byID(Find(in), "gitignore")
	if !strings.Contains(r.What, "add") {
		t.Fatalf("top-up: %+v", r)
	}
	Run([]Repair{r}, true)
	data, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# mine\nchannels/\nmemory/\n\n# added by DHI\n") ||
		strings.Count(string(data), "channels/") != 1 || !strings.Contains(string(data), "episodes/") {
		t.Fatalf("topped up:\n%s", data)
	}
}

func TestGitignoreSkipsWhatGitAlreadyIgnores(t *testing.T) {
	in := fixture(t)
	in.Ignored = func(string) bool { return true } // a root .gitignore covers .dhi/
	if _, ok := byID(Find(in), "gitignore"); ok {
		t.Fatal("repair offered though git ignores everything")
	}
}

func TestLibraryRefsAskThenDropOnlyWhatIsMissing(t *testing.T) {
	in := fixture(t)
	a := &manifest.Agent{ID: "ada", Name: "Ada", Model: manifest.ModelDefault, Role: "fixer", Persona: "gone-persona",
		Skills: []string{"docs", "gone-skill"}, Tools: []string{"read"}}
	if err := in.Org.CreateAgent(in.WS, a); err != nil {
		t.Fatal(err)
	}
	r, ok := byID(Find(in), "library-refs:ada")
	if !ok || r.Safe || !strings.Contains(r.What, "persona gone-persona, skill gone-skill") {
		t.Fatalf("repair %+v", r)
	}
	if res := Run(Find(in), true); len(res.Done) != 1 { // safe only: gitignore
		t.Fatalf("safe run touched an agent: %+v", res)
	}
	if err := r.Apply(); err != nil {
		t.Fatal(err)
	}
	roster, _ := org.LoadRoster(in.WS)
	got := roster[0]
	if got.Role != "fixer" || got.Persona != "" || len(got.Skills) != 1 || got.Skills[0] != "docs" {
		t.Fatalf("fixed %+v", got)
	}
}

func TestDanglingTeamLeadGoesToYou(t *testing.T) {
	in := fixture(t)
	for _, id := range []string{"ada", "bob"} {
		in.Org.CreateAgent(in.WS, &manifest.Agent{ID: id, Name: id, Model: manifest.ModelDefault, Tools: []string{"read"}})
	}
	if err := in.Org.CreateTeam("core", "ada", []string{"ada", "bob"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := byID(Find(in), "team:core"); ok {
		t.Fatal("healthy team repaired")
	}
	in.Org.DeleteAgent(in.WS, "ada")
	r, ok := byID(Find(in), "team:core")
	if !ok || r.Safe || r.What != "team core: its lead ada is gone: you lead it" {
		t.Fatalf("repair %+v", r)
	}
	if err := r.Apply(); err != nil {
		t.Fatal(err)
	}
	tm, _ := in.Org.Team("core")
	if tm.Lead != org.Human || len(tm.Members) != 1 || tm.Members[0] != "bob" {
		t.Fatalf("team %+v", tm)
	}
}

// The old toolchain is deleted only on yes, and only a dhi/toolchain dir.
func TestLegacyToolchainAsksBeforeDeleting(t *testing.T) {
	in := fixture(t)
	base := t.TempDir()
	dir := filepath.Join(base, "dhi", "toolchain")
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "bin", "go"), []byte("x"), 0o755)

	in.LegacyToolchain = dir
	rs := Find(in)
	r, ok := byID(rs, "legacy-toolchain")
	if !ok || r.Safe || !strings.Contains(r.What, dir) {
		t.Fatalf("repair = %+v", r)
	}
	Run(rs, true) // safe-only: kept
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("deleted without a yes")
	}
	if err := r.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("not deleted on yes")
	}
	for _, other := range []string{base, filepath.Join(base, "toolchain")} {
		if _, ok := legacyToolchain(other); ok {
			t.Fatalf("%s must never qualify", other)
		}
	}
}

// A member added after the user kept their identity to the workspace gets
// it too; a repo with its own [user] is left alone.
func TestLocalIdentityReachesNewMembers(t *testing.T) {
	in := fixture(t)
	var set []string
	in.LocalIdentity = &setup.Identity{Name: "Ada", Email: "ada@x.io"}
	in.SetLocalIdentity = func(dir string) error { set = append(set, dir); return nil }
	ms := in.WS.Members()
	if len(ms) == 0 {
		t.Skip("fixture has no members")
	}
	os.MkdirAll(filepath.Join(ms[0].Path, ".git"), 0o755)
	os.WriteFile(filepath.Join(ms[0].Path, ".git", "config"), []byte("[core]\n\tbare = false\n"), 0o644)
	r, ok := byID(Find(in), "local-identity/"+ms[0].Name)
	if !ok || !r.Safe || !strings.Contains(r.What, "Ada") {
		t.Fatalf("repair = %+v", r)
	}
	_ = r.Apply()
	if len(set) != 1 || set[0] != ms[0].Path {
		t.Fatalf("set = %v", set)
	}
	os.WriteFile(filepath.Join(ms[0].Path, ".git", "config"), []byte("[user]\n\tname = Ada\n"), 0o644)
	if _, ok := byID(Find(in), "local-identity/"+ms[0].Name); ok {
		t.Fatal("a repo with an identity must be left alone")
	}
}
