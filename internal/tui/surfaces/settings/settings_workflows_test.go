package settings

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
)

func TestWorkflowsSectionBrowseAuthorDefault(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.sec = secWorkflows

	// Browse: only the builtin at first.
	rows := m.workflowRows()
	if len(rows) != 1 || rows[0].slug != "feature" || rows[0].local {
		t.Fatalf("initial rows = %+v", rows)
	}

	// Author a new workflow via the strict form.
	feed(m, "n")
	typeDialog(m, "ci", "CI flow")
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("dialog did not close: %+v", m.dlg)
	}
	if _, err := workflow.Load(ws.Root, "ci"); err != nil {
		t.Fatalf("workflow not created: %v", err)
	}
	rows = m.workflowRows()
	if len(rows) != 2 || rows[0].slug != "ci" || !rows[0].local {
		t.Fatalf("rows after author = %+v", rows)
	}

	// A bad slug is refused by the form, not persisted.
	feed(m, "n")
	typeDialog(m, "Bad Slug")
	feed(m, "enter")
	if m.dlg == nil {
		t.Fatal("bad slug must keep the form open with an error")
	}
	feed(m, "esc") // dismiss the refused form

	// Set the selected workflow (ci) as the workspace default, then clear.
	m.wfCur = 0
	feed(m, "d")
	if def, _ := workflow.WorkspaceDefault(ws.Root); def != "ci" {
		t.Fatalf("default not set: %q", def)
	}
	feed(m, "d")
	if def, _ := workflow.WorkspaceDefault(ws.Root); def != "" {
		t.Fatalf("default not cleared: %q", def)
	}

	// Preview opens a read-only modal listing the steps.
	feed(m, "v")
	if m.dlg == nil || m.dkind != dlgDisplay {
		t.Fatalf("preview did not open a display modal: %+v", m.dlg)
	}
}
