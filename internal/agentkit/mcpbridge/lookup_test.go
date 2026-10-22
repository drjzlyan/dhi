package mcpbridge

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/credstore"
)

// F-104: a secret saved "for this workspace" reaches that workspace's MCP
// servers (it used to be looked up globally only), and no other's.
func TestLookupForPrefersTheWorkspaceSecret(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MCP_TEST_TOKEN", "")
	st, err := credstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetIn("/ws/one", "MCP_TEST_TOKEN", "workspace-value"); err != nil {
		t.Fatal(err)
	}
	if v, ok := LookupFor("/ws/one")("MCP_TEST_TOKEN"); !ok || v != "workspace-value" {
		t.Fatalf("workspace lookup = %q, %v", v, ok)
	}
	if _, ok := LookupFor("/ws/two")("MCP_TEST_TOKEN"); ok {
		t.Fatal("another workspace must not see it")
	}
}
