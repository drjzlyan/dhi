package brief

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := Load(root); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	b := Brief{Name: "shop", Summary: "A store.", Process: "line one\nline two", Dont: "touch prod"}
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(root)
	if err != nil || !ok || got != b {
		t.Fatalf("got %+v ok=%v err=%v", got, ok, err)
	}
}

func TestLoadRefusesUnknownKeysAndBadSchema(t *testing.T) {
	root := t.TempDir()
	write(t, root, File, "schema = 1\n[project]\nbogus = \"x\"\n")
	if _, _, err := Load(root); err == nil || !strings.Contains(err.Error(), "project.bogus") {
		t.Fatalf("err = %v", err)
	}
	write(t, root, File, "schema = 2\n")
	if _, _, err := Load(root); err == nil || !strings.Contains(err.Error(), "schema 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetAndGet(t *testing.T) {
	var b Brief
	if err := b.Set("goals", "  ship v1  "); err != nil || b.Get("goals") != "ship v1" {
		t.Fatalf("set goals: %v %q", err, b.Goals)
	}
	if err := b.Set("budget", "x"); err == nil || !strings.Contains(err.Error(), "valid: name") {
		t.Fatalf("unknown field: %v", err)
	}
	for _, f := range Fields {
		if b.ptr(f.Key) == nil {
			t.Errorf("field %s has no storage", f.Key)
		}
		if len(f.Label) > 10 {
			t.Errorf("label %q is wider than a form label", f.Label)
		}
	}
}

func TestRenderSkipsEmptyAndIndentsMultiline(t *testing.T) {
	if (Brief{}).Render() != "" {
		t.Fatal("empty brief renders")
	}
	got := Brief{Name: "shop", Process: "card\nPR"}.Render()
	for _, want := range []string{"ground truth", "Project: shop", "How we work:\n  card\n  PR"} {
		if !strings.Contains(got, want) {
			t.Errorf("render lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Goals") {
		t.Errorf("empty field rendered:\n%s", got)
	}
}

func TestDetectGoRepo(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module github.com/acme/widget\n\ngo 1.24\n")
	write(t, root, "README.md", "# Widget\n\n[![ci](x)](y)\n\nWidget turns **gears** into [sprockets](http://x).\nIt is fast.\n\n## Install\n")
	write(t, root, ".github/workflows/ci.yml", "on: push\n")
	b := Detect(root, []string{root})
	if b.Name != "widget" {
		t.Errorf("name %q", b.Name)
	}
	if b.Summary != "Widget turns gears into sprockets. It is fast." {
		t.Errorf("summary %q", b.Summary)
	}
	if b.Stack != "Go" || !strings.Contains(b.Testing, "go test ./...") {
		t.Errorf("stack %q testing %q", b.Stack, b.Testing)
	}
	if !strings.Contains(b.Process, "GitHub Actions") || b.Review != DefaultReview {
		t.Errorf("process %q review %q", b.Process, b.Review)
	}
}

func TestDetectPolyglotMembers(t *testing.T) {
	root := t.TempDir()
	web, api := filepath.Join(root, "web"), filepath.Join(root, "api")
	write(t, web, "package.json", `{"name":"web","dependencies":{"react":"18","next":"14"},"devDependencies":{"typescript":"5"},"scripts":{"test":"vitest"}}`)
	write(t, api, "pyproject.toml", "[project]\nname = \"api\"\ndependencies = [\"fastapi\", \"pytest\"]\n")
	b := Detect(root, []string{web, api})
	if b.Name != filepath.Base(root) {
		t.Errorf("multi-member name %q", b.Name)
	}
	for _, want := range []string{"Node.js", "Next.js", "React", "TypeScript", "Python", "FastAPI"} {
		if !strings.Contains(b.Stack, want) {
			t.Errorf("stack %q lacks %s", b.Stack, want)
		}
	}
	if !strings.Contains(b.Testing, "(in web)") {
		t.Errorf("testing %q", b.Testing)
	}
}

func TestDetectEmptyDir(t *testing.T) {
	b := Detect(t.TempDir(), nil)
	if b.Summary != "" || b.Stack != "" || b.Testing != "" || b.Process != DefaultProcess {
		t.Errorf("got %+v", b)
	}
}
