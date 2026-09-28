package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/drjzlyan/dhi/internal/mcp"
)

type stubHandler struct {
	name   string
	tools  []mcp.ToolInfo
	called *string
}

func (s stubHandler) ProtocolVersion() string      { return mcp.ProtocolVersion }
func (s stubHandler) ServerInfo() (string, string) { return s.name, "1" }
func (s stubHandler) Tools() []mcp.ToolInfo        { return s.tools }
func (s stubHandler) CallTool(_ context.Context, n string, _ json.RawMessage) (string, bool, error) {
	for _, ti := range s.tools {
		if ti.Name == n {
			*s.called = n
			return s.name + " out", false, nil
		}
	}
	return "unknown tool " + n, true, nil
}

func TestCompositeHandlerRoutesByPrefix(t *testing.T) {
	called := ""
	dhi := stubHandler{name: "dhi", tools: []mcp.ToolInfo{{Name: "read"}}, called: &called}
	bridge := stubHandler{name: "bridge", tools: []mcp.ToolInfo{{Name: "mcp__fs__read"}}, called: &called}
	c := compositeHandler{dhi: dhi, bridge: bridge}

	if got := c.Tools(); len(got) != 2 {
		t.Fatalf("merged tools = %+v", got)
	}
	out, _, _ := c.CallTool(context.Background(), "read", nil)
	if called != "read" || out != "dhi out" {
		t.Fatalf("dhi route: called=%q out=%q", called, out)
	}
	out, _, _ = c.CallTool(context.Background(), "mcp__fs__read", nil)
	if called != "mcp__fs__read" || out != "bridge out" {
		t.Fatalf("bridge route: called=%q out=%q", called, out)
	}
	if _, isErr, _ := c.CallTool(context.Background(), "nope", nil); !isErr {
		t.Fatal("unknown tool not refused")
	}
}
