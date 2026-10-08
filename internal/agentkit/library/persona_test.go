package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestBuiltinPersonasLoadAndRender(t *testing.T) {
	s := Open(nil)
	if len(s.Warnings()) != 0 {
		t.Fatalf("builtin warnings: %v", s.Warnings())
	}
	want := []string{"friendly", "mentor", "meticulous", "pragmatic", "terse"}
	var got []string
	for _, e := range s.Personas() {
		got = append(got, e.Slug)
		if e.Kind != "persona" || e.Source != "builtin" || e.Description == "" {
			t.Errorf("entry = %+v", e)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("personas = %v, want %v", got, want)
	}
	for _, slug := range want {
		p, _ := s.Persona(slug)
		out := p.Render()
		if !strings.HasPrefix(out, "Voice: ") || !strings.Contains(out, "\n- ") {
			t.Errorf("%s renders badly:\n%s", slug, out)
		}
	}
}

func TestPersonaRenderIsDeterministicAndOmitsEmpty(t *testing.T) {
	p := &Persona{Tone: "calm", Verbosity: VerbosityTerse, Traits: []string{"a", "b"}, Guidance: "Extra."}
	want := "Voice: calm. Keep replies as short as the task allows.\n- a\n- b\n\nExtra."
	if got := p.Render(); got != want || p.Render() != got {
		t.Fatalf("render = %q, want %q", got, want)
	}
	if (&Persona{}).Render() != "" || (*Persona)(nil).Render() != "" {
		t.Fatal("an empty persona must render nothing")
	}
}

func TestParsePersonaIsStrict(t *testing.T) {
	bad := map[string]string{
		"unknown key":   "schema = 1\ndescription = \"d\"\nmood = \"x\"\n",
		"no desc":       "schema = 1\ntone = \"t\"\n",
		"bad schema":    "schema = 2\ndescription = \"d\"\n",
		"bad verbosity": "schema = 1\ndescription = \"d\"\nverbosity = \"loud\"\n",
		"too many":      "schema = 1\ndescription = \"d\"\ntraits = [\"1\",\"2\",\"3\",\"4\",\"5\",\"6\",\"7\",\"8\",\"9\"]\n",
	}
	for name, doc := range bad {
		if _, err := ParsePersona("x", []byte(doc)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestLocalPersonaShadowsBuiltinAndBadCardIsAWarning(t *testing.T) {
	root := t.TempDir()
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, workspace.DirPersonas)
	os.WriteFile(filepath.Join(dir, "terse.toml"),
		[]byte("schema = 1\ndescription = \"house terse\"\ntone = \"clipped\"\nverbosity = \"terse\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("schema = 1\n"), 0o644)

	s := Open(ws)
	p, _ := s.Persona("terse")
	if p.Tone != "clipped" || s.Source("persona", "terse") != "local" {
		t.Fatalf("local card did not shadow: %+v source=%q", p, s.Source("persona", "terse"))
	}
	if _, ok := s.Persona("broken"); ok {
		t.Fatal("malformed card must not load")
	}
	found := false
	for _, w := range s.Warnings() {
		if strings.Contains(w, "broken.toml") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no warning naming broken.toml: %v", s.Warnings())
	}
}

func TestWritePersonaRoundTrips(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, _ := workspace.Load(root)
	s := Open(ws)
	in := &Persona{Slug: "house", Description: "House style", Tone: "plain", Verbosity: VerbosityBalanced,
		Traits: []string{"short sentences"}, Guidance: "No emoji."}
	if err := s.WritePersona(ws, in); err != nil {
		t.Fatal(err)
	}
	out, ok := Open(ws).Persona("house")
	if !ok || out.Render() != in.Render() {
		t.Fatalf("round trip: %+v", out)
	}
	if err := s.WritePersona(ws, &Persona{Slug: "Bad Slug", Description: "x"}); err == nil {
		t.Fatal("bad slug accepted")
	}
}
