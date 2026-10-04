package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestEditorRailScrollbar(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		p := filepath.Join(alpha, fmt.Sprintf("f%02d.go", i))
		if err := os.WriteFile(p, []byte("package a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema = 1\n\n[members.alpha]\npath = \"alpha\"\n"
	if err := os.WriteFile(filepath.Join(root, ".dhi", "workspace.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := New("test", ws)
	m.roots[0].toggle() // expand the member so its files become rows
	m.refreshRows()

	m.Resize(100, 60) // window 56 > 31 rows → no bar
	if strings.Contains(ansi.Strip(m.View()), "█") {
		t.Fatal("no scrollbar expected when the rail fits")
	}
	m.Resize(100, 12) // window 8 < 31 rows → overflow
	if !strings.Contains(ansi.Strip(m.View()), "█") {
		t.Fatal("expected a right-edge scrollbar on the editor files rail")
	}
}
