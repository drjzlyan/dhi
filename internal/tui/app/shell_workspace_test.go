package app

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/placeholder"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/workspace"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	wspkg "github.com/drjzlyan/dhi/internal/workspace"
)

// TestShellRealWorkspaceGolden pins the chrome around a real workspace
// floor (F-093): tab bar, the section pane and the status bar that owns
// the hints. The other shell goldens show the not-a-workspace splash.
func TestShellRealWorkspaceGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	theme.MotionForTest(t, false)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := wspkg.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := wspkg.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{
		{"audit-log", "Audit log for admin actions"},
		{"csv-export", "CSV export for reports"},
		{"login-timeout", "Fix login timeout on slow networks"},
	} {
		if err := ts.Create(c[0], c[1], "", ""); err != nil {
			t.Fatal(err)
		}
	}
	a := New("0.1.0",
		workspace.New("0.1.0", ws, workspace.Deps{Tasks: ts}),
		placeholder.New("editor", "Editor", "M2", "Files."),
		placeholder.New("settings", "Settings", "M2+", "Everything configurable."))
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	golden.Snapshot(t, "shell_real_workspace_120x36", a.compose())
	golden.AssertBgIntegrity(t, "shell_real_workspace", a.compose())
}
