package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/trust"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// TestTrustRow is F-148: a member nobody trusted is a Warn that names it
// and the way out; trusting it turns the row OK.
func TestTrustRow(t *testing.T) {
	root := setupWorkspaceRoot(t, true)
	rec := filepath.Join(t.TempDir(), "trust.json")
	old := trustPath
	trustPath = func() (string, error) { return rec, nil }
	defer func() { trustPath = old }()

	c, ok := statusOf(Trust(root), "workspace/trust")
	if !ok || c.Status != Warn || !strings.Contains(c.Detail, "restricted") || !strings.Contains(c.Detail, ":trust") {
		t.Fatalf("untrusted = %+v (found=%v)", c, ok)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.New(rec).Trust(ws.Members()[0].Path); err != nil {
		t.Fatal(err)
	}
	if c, _ := statusOf(Trust(root), "workspace/trust"); c.Status != OK {
		t.Fatalf("trusted = %+v", c)
	}
	if err := os.WriteFile(rec, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ := statusOf(Trust(root), "workspace/trust"); c.Status != Warn || !strings.Contains(c.Detail, "not valid") {
		t.Fatalf("corrupt = %+v", c)
	}
}
