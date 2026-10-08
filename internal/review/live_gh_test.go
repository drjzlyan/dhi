package review

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveGHAcceptsRemoteURLForms runs READ-ONLY calls through a real gh
// to prove the repo normalisation (a fake accepted anything, which is how
// `repos/<remote URL>/…` shipped broken). Gate-style like DHI_SMOKE_DAP:
//
//	DHI_SMOKE_GH=$(command -v gh) go test ./internal/review -run TestLiveGH -v
func TestLiveGHAcceptsRemoteURLForms(t *testing.T) {
	bin := os.Getenv("DHI_SMOKE_GH")
	if bin == "" {
		t.Skip("set DHI_SMOKE_GH to a gh binary to run against real GitHub (read-only)")
	}
	g := NewGHCLI(bin)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, remote := range []string{
		"https://github.com/cli/cli.git", "git@github.com:cli/cli.git", "cli/cli",
	} {
		if _, err := g.ReviewComments(ctx, remote, "1"); err != nil {
			t.Errorf("ReviewComments(%q): %v", remote, err)
		}
		if _, err := g.IssueComments(ctx, remote, "1"); err != nil {
			t.Errorf("IssueComments(%q): %v", remote, err)
		}
	}
	if login, err := g.Login(ctx); err != nil || login == "" {
		t.Errorf("Login = %q, %v", login, err)
	}
}
