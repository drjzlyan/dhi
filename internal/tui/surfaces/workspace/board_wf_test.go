package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
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
