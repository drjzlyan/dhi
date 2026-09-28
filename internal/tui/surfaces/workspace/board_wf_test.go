package workspace

import (
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
	body := ansi.Strip(m.reposBody(80))
	if !strings.Contains(body, "dependencies") || !strings.Contains(body, "→ alpha (api)") {
		t.Fatalf("dependency view missing the edge:\n%s", body)
	}
	// Dangling edge flagged by name.
	if err := ws.SetDependencies([]workspace.Dependency{{From: "beta", To: "ghost", Kind: "build"}}); err != nil {
		t.Fatal(err)
	}
	body = ansi.Strip(m.reposBody(80))
	if !strings.Contains(body, "member missing") {
		t.Fatalf("dangling edge not flagged:\n%s", body)
	}
}
