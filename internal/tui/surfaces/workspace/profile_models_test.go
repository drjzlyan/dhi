package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/profile"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestCompactProfileShowsModelBreakdown(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	p := &profile.Profile{
		ID: "scout", Found: true,
		TasksOpen: []tasks.Task{{Slug: "x", Runs: []tasks.Run{
			{Agent: "scout", Model: "gpt-5", Status: tasks.RunOK, HasCost: true, CostUSD: 0.10},
			{Agent: "scout", Model: "gpt-5", Status: tasks.RunError, HasCost: true, CostUSD: 0.03},
			{Agent: "scout", Model: "sonnet", Status: tasks.RunOK, HasCost: true, CostUSD: 0.02},
			{Agent: "other", Model: "gpt-5", Status: tasks.RunOK, HasCost: true, CostUSD: 9.0},
		}}},
	}
	out := ansi.Strip(strings.Join(compactProfileLines(p), "\n"))
	if !strings.Contains(out, "gpt-5 · 2 runs") {
		t.Fatalf("gpt-5 breakdown missing:\n%s", out)
	}
	if !strings.Contains(out, "sonnet · 1 runs") {
		t.Fatalf("sonnet breakdown missing:\n%s", out)
	}
	// The other agent's run is excluded.
	if strings.Contains(out, "· 3 runs") || strings.Contains(out, "$9") {
		t.Fatalf("cross-agent run leaked into the breakdown:\n%s", out)
	}
}
