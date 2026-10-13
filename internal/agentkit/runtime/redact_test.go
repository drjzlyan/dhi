package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/mcp"
)

type echoHandler struct{}

func (echoHandler) ProtocolVersion() string      { return mcp.ProtocolVersion }
func (echoHandler) ServerInfo() (string, string) { return "t", "1" }
func (echoHandler) Tools() []mcp.ToolInfo        { return nil }
func (echoHandler) CallTool(_ context.Context, _ string, args json.RawMessage) (string, bool, error) {
	return "out: " + string(args), false, nil
}

// F-088: a tool result that echoes a secret reaches the agent redacted.
func TestToolResultsAreRedacted(t *testing.T) {
	h := redactHandler{Handler: echoHandler{}, redact: func(s string) string {
		return strings.ReplaceAll(s, "sk-live-123456", "‹secret:KEY›")
	}}
	out, _, err := h.CallTool(context.Background(), "run", json.RawMessage(`"sk-live-123456"`))
	if err != nil || strings.Contains(out, "sk-live") || !strings.Contains(out, "‹secret:KEY›") {
		t.Fatalf("out = %q err = %v", out, err)
	}
}
