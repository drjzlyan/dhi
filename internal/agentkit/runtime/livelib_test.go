package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// The crew re-reads the behaviour library when a card changes on disk, so a
// persona edited in Settings governs the next turn (F-052).
func TestLibraryIsReReadWhenACardChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{cfg: Config{WS: ws}}

	first := r.lib()
	if _, ok := first.Persona("night-owl"); ok {
		t.Fatal("persona exists before it is written")
	}
	if r.lib() != first {
		t.Fatal("an unchanged library was re-read on every turn")
	}

	dir := filepath.Join(root, workspace.DirPersonas)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	card := "schema = 1\ndescription = \"d\"\ntone = \"wry\"\n"
	if err := os.WriteFile(filepath.Join(dir, "night-owl.toml"), []byte(card), 0o644); err != nil {
		t.Fatal(err)
	}
	p, ok := r.lib().Persona("night-owl")
	if !ok || p.Tone != "wry" {
		t.Fatalf("a new persona is not live: %+v ok=%v", p, ok)
	}

	// Editing an existing card (same name; make the mtime differ).
	edited := "schema = 1\ndescription = \"d\"\ntone = \"gentle and slow\"\n"
	path := filepath.Join(dir, "night-owl.toml")
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if p, _ := r.lib().Persona("night-owl"); p == nil || p.Tone != "gentle and slow" {
		t.Fatalf("an edited persona is not live: %+v", p)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.lib().Persona("night-owl"); ok {
		t.Fatal("a deleted persona is still in force")
	}
}

func TestNilWorkspaceKeepsNoLibrary(t *testing.T) {
	if (&Runtime{}).lib() != nil {
		t.Fatal("a runtime without a workspace has a library")
	}
}
