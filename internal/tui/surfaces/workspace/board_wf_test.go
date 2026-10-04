package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestBoardWorkflowLine(t *testing.T) {
	root := t.TempDir()

	if _, ok := boardWorkflowLine(tasks.Task{}, root); ok {
		t.Fatal("no workflow → no line")
	}
	line, ok := boardWorkflowLine(tasks.Task{Workflow: "feature"}, root)
	if !ok || !strings.Contains(line, "next: worktree_create") || !strings.Contains(line, "block") {
		t.Fatalf("line = %q ok=%v", line, ok)
	}
	line, ok = boardWorkflowLine(tasks.Task{Workflow: "feature", TestsPass: true, Bypasses: []tasks.Bypass{{Step: "review", Reason: "x"}}, ChangeSets: []tasks.ChangeSet{{Member: "api", Branch: "b", Path: "p"}}}, root)
	if !ok || !strings.Contains(line, "complete") {
		t.Fatalf("complete line = %q ok=%v", line, ok)
	}
	if line, _ := boardWorkflowLine(tasks.Task{Workflow: "ghost"}, root); !strings.Contains(line, "missing") {
		t.Fatalf("missing line = %q", line)
	}
}

func TestReposBodyRendersDependencies(t *testing.T) {
	m, ws := newSurface(t)
	if err := ws.SetDependencies([]workspace.Dependency{{From: "beta", To: "alpha", Kind: "api"}}); err != nil {
		t.Fatal(err)
	}
	body := ansi.Strip(m.reposBody(80, 40))
	if !strings.Contains(body, "dependencies") || !strings.Contains(body, "→ alpha (api)") {
		t.Fatalf("dependency view missing the edge:\n%s", body)
	}
	// Dangling edge flagged by name.
	if err := ws.SetDependencies([]workspace.Dependency{{From: "beta", To: "ghost", Kind: "build"}}); err != nil {
		t.Fatal(err)
	}
	body = ansi.Strip(m.reposBody(80, 40))
	if !strings.Contains(body, "member missing") {
		t.Fatalf("dangling edge not flagged:\n%s", body)
	}
}

func TestReposPaneScrollbar(t *testing.T) {
	m, ws := newSurface(t)
	for i := 0; i < 12; i++ {
		name := "m" + string(rune('a'+i))
		if err := os.MkdirAll(filepath.Join(ws.Root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := ws.AddMember(name, name); err != nil {
			t.Fatal(err)
		}
	}
	m.sec = secRepos
	m.Resize(100, 40)
	if strings.Contains(m.View(), "█") {
		t.Fatal("no scrollbar expected when members fit")
	}
	m.Resize(100, 6) // body budget 3 < 12 members → overflow
	if !strings.Contains(m.View(), "█") {
		t.Fatal("expected a right-edge scrollbar on REPOS")
	}
	// Cursor follow: moving down past the window advances the offset.
	m.cursors[secRepos] = 0
	m.offsets[secRepos] = 0
	m.reposBody(80, 3)
	if m.offsets[secRepos] != 0 {
		t.Fatalf("offset = %d, want 0", m.offsets[secRepos])
	}
	m.cursors[secRepos] = 10
	m.reposBody(80, 3)
	if m.offsets[secRepos] == 0 {
		t.Fatal("offset did not follow the cursor")
	}
}
