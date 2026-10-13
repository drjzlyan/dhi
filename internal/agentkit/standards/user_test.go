package standards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-090: your instructions apply in every workspace, after the built-ins
// and before the workspace's own.
func TestUserLayerAppliesBeforeTheWorkspace(t *testing.T) {
	UserPath = filepath.Join(t.TempDir(), "standards.toml")
	t.Cleanup(func() { UserPath = "" })
	if err := SaveUser([]string{"Answer in British English", " "}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := Save(root, []string{"Use pnpm"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := Resolve(root, "anvil", nil)
	mine, ws := strings.Index(got, "British English"), strings.Index(got, "Use pnpm")
	if mine < 0 || ws < 0 || mine > ws {
		t.Fatalf("order wrong:\n%s", got)
	}
	// A second workspace with nothing of its own still gets yours.
	if !strings.Contains(Resolve(t.TempDir(), "anvil", nil), "British English") {
		t.Fatal("not applied in another workspace")
	}
	if err := os.WriteFile(UserPath, []byte("schema = 1\nbogus = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err == nil {
		t.Fatal("malformed user file passed Check")
	}
}
