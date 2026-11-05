package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestFallbacksRow(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	installed := map[string]bool{"claude": true, "codex": true}
	lookPath = func(n string) (string, error) {
		if installed[n] {
			return "/bin/" + n, nil
		}
		return "", fmt.Errorf("not found")
	}
	root := wsFixture(t)
	dir := filepath.Join(root, workspace.DirAgents)
	write := func(id, extra string) {
		doc := "schema = 7\nname = \"S\"\nmodel = \"m\"\nengine = \"cli:claude\"\n" + extra
		if err := os.WriteFile(filepath.Join(dir, id+".toml"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a", "fallback_engine = \"cli:codex\"\n")
	write("b", "fallback_engine = \"none\"\n")
	write("c", "")
	got := Fallbacks(root)
	if len(got) != 1 || got[0].Name != "agents/fallback" || got[0].Status != OK {
		t.Fatalf("checks = %+v", got)
	}
	for _, want := range []string{"a: cli:claude → cli:codex (resolves)", "b: no fallback (none, by choice)", "c: no fallback (no fallback chosen yet"} {
		if !strings.Contains(got[0].Detail, want) {
			t.Errorf("detail %q lacks %q", got[0].Detail, want)
		}
	}

	installed["codex"] = false // the chosen fallback is gone
	got = Fallbacks(root)
	if got[0].Status != Warn || !strings.Contains(got[0].Detail, "codex is not installed") {
		t.Fatalf("missing fallback = %+v", got[0])
	}
}
