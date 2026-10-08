// Package layoutcontract pins the responsiveness contract (F-054): every
// surface, in every section, with and without a workspace, renders exactly
// the rows it was given and never a cell wider — from a cramped 40x12
// split to a 200x50 monitor. The shell also fits bodies (kit.Fit), but
// that is a safety net: a surface that overflows loses its bottom rows
// (usually the hint bar), so the surfaces themselves must comply.
package layoutcontract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tui/app"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/editor"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/ideator"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/reviewer"
	settingsview "github.com/drjzlyan/dhi/internal/tui/surfaces/settings"
	workspaceview "github.com/drjzlyan/dhi/internal/tui/surfaces/workspace"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// sizes are body sizes (the shell gives surfaces width x height-2).
var sizes = [][2]int{{40, 10}, {60, 18}, {80, 22}, {120, 33}, {200, 48}}

// TestNoPanicBeforeResizeOrWhenTiny: the first frame renders before any
// WindowSizeMsg (0x0), and a split pane can be absurdly small. Every
// surface must survive both (the output is clipped by the shell).
func TestNoPanicBeforeResizeOrWhenTiny(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	for _, withWS := range []bool{false, true} {
		var ws *workspace.Workspace
		if withWS {
			ws = fixtureWS(t)
		}
		for _, s := range build(t, ws) {
			s.Init()
			_ = s.View() // never resized
			for _, sz := range [][2]int{{0, 0}, {1, 1}, {10, 3}, {20, 5}} {
				s.Resize(sz[0], sz[1])
				_ = s.View()
				for i := 0; i < 6; i++ {
					s.HandleKey("]")
					_ = s.View()
				}
			}
		}
	}
	a := app.New("test", build(t, fixtureWS(t))...)
	_ = a.View() // shell before resize
	for _, sz := range [][2]int{{1, 1}, {10, 3}, {20, 5}} {
		a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		_ = a.View()
		a.Update(key("?"))
		_ = a.View()
		a.Update(key("esc"))
	}
}

func fixtureWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfor i := 0; i < 3; i++ {\n\t\tfmt.Println(\"tab-indented\", i)\t// trailing\n\t}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema = 1\n\n[members.api]\npath = \"api\"\n"
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// build returns every app surface for ws (nil = launched outside a workspace).
func build(t *testing.T, ws *workspace.Workspace) []surfaces.Surface {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	return []surfaces.Surface{
		workspaceview.New("test", ws, workspaceview.Deps{}),
		editor.New("test", ws),
		ideator.New("test", ws, ideator.Deps{}),
		reviewer.New("test", ws, reviewer.Deps{}),
		settingsview.New(settings.Defaults(), cfgPath, settingsview.Deps{WS: ws}),
	}
}

func check(t *testing.T, name string, s surfaces.Surface, w, h int) {
	t.Helper()
	lines := strings.Split(s.View(), "\n")
	if len(lines) != h {
		t.Errorf("%s at %dx%d: %d rows, want exactly %d", name, w, h, len(lines), h)
	}
	for i, l := range lines {
		if lw := ansi.Width(l); lw > w {
			t.Errorf("%s at %dx%d: row %d is %d cells wide: %q", name, w, h, i, lw, ansi.Strip(l))
			break
		}
		// A raw tab measures 0 cells here but jumps to the terminal's next
		// tab stop, pushing the row past the panel edge.
		if strings.ContainsRune(l, '\t') {
			t.Errorf("%s at %dx%d: row %d contains a raw tab: %q", name, w, h, i, ansi.Strip(l))
			break
		}
	}
}

// TestEditorStatesFillTheViewport walks the editor's layout-changing
// states: a buffer open (tab strip + gutter), the git panel and the
// terminal drawer stacked below.
func TestEditorStatesFillTheViewport(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ws := fixtureWS(t)
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		ed := editor.New("test", ws)
		ed.Init()
		ed.Resize(w, h)
		check(t, "editor tree", ed, w, h)
		if n := ed.OpenPaths(filepath.Join(ws.Root, "api", "main.go")); n != 1 {
			t.Fatalf("OpenPaths opened %d files", n)
		}
		check(t, "editor buffer", ed, w, h)
		ed.HandleKey("ctrl+j")
		check(t, "editor buffer+git", ed, w, h)
		ed.HandleKey("ctrl+j")
	}
}

func TestEverySurfaceFillsItsViewport(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	for _, withWS := range []bool{false, true} {
		var ws *workspace.Workspace
		if withWS {
			ws = fixtureWS(t)
		}
		for _, s := range build(t, ws) {
			s.Init()
			for _, sz := range sizes {
				w, h := sz[0], sz[1]
				s.Resize(w, h)
				name := fmt.Sprintf("%s(ws=%v)", s.Meta().ID, withWS)
				check(t, name, s, w, h)
				// Walk the rail sections: "]" is the shared next-section key.
				for i := 0; i < 10; i++ {
					if !s.HandleKey("]") {
						break
					}
					check(t, fmt.Sprintf("%s section+%d", name, i+1), s, w, h)
				}
			}
		}
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Text: s, Code: r[0]}
}

// TestShellScreensFitTheTerminal checks the composed screen — tab bar,
// body, statusline and every overlay — at each terminal size.
func TestShellScreensFitTheTerminal(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ws := fixtureWS(t)
	for _, sz := range [][2]int{{40, 12}, {60, 20}, {80, 24}, {120, 35}, {200, 50}} {
		w, h := sz[0], sz[1]
		a := app.New("test", build(t, ws)...)
		a.SetWelcome(nil)
		a.Update(tea.WindowSizeMsg{Width: w, Height: h})
		screen := func(name string) {
			t.Helper()
			lines := strings.Split(a.View().Content, "\n")
			if len(lines) != h {
				t.Errorf("%s at %dx%d: %d rows, want %d", name, w, h, len(lines), h)
			}
			for i, l := range lines {
				if lw := ansi.Width(l); lw > w {
					t.Errorf("%s at %dx%d: row %d is %d cells wide: %q", name, w, h, i, lw, ansi.Strip(l))
					break
				}
			}
		}
		if !strings.Contains(ansi.Strip(a.View().Content), "Welcome to DHI") {
			t.Fatalf("welcome card missing at %dx%d", w, h)
		}
		screen("welcome")
		a.Update(key("esc"))
		for v := 1; v <= 5; v++ {
			a.Update(key(fmt.Sprint(v)))
			screen(fmt.Sprintf("view %d", v))
			a.Update(key("?"))
			screen(fmt.Sprintf("view %d help", v))
			if !strings.Contains(ansi.Strip(a.View().Content), "global keys") {
				t.Fatalf("view %d at %dx%d: help overlay did not open", v, w, h)
			}
			a.Update(key("/"))
			a.Update(key("s"))
			screen(fmt.Sprintf("view %d help filtered", v))
			a.Update(key("esc"))
			a.Update(key("esc"))
			a.Update(key("ctrl+p"))
			screen(fmt.Sprintf("view %d palette", v))
			a.Update(key("esc"))
		}
	}
}
