// Command demo seeds a realistic DHI workspace for screenshots and demos:
// two member repos with history and a feature branch, the "squad"
// starter team, a board in every state, a lively #general, a round-table
// session and a review with agent suggestions. Everything goes through
// DHI's own stores, so the TUI shows real data, not mock-ups.
//
//	go run ./scripts/demo <dir>
//
// The hermetic git from the DHI toolchain makes the commits and the
// review worktree; run `dhi` once first so the toolchain is installed.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/starter"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/workspace"
	"github.com/drjzlyan/dhi/internal/worktrees"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/demo <dir>")
		os.Exit(2)
	}
	if err := seed(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func seed(root string) error {
	ctx := context.Background()
	if _, err := os.Stat(root); err == nil {
		return fmt.Errorf("%s exists; pick a fresh directory", root)
	}
	tc, err := toolchain.DefaultRoot()
	if err != nil {
		return err
	}
	mgr := toolchain.New(tc)
	git := gitcore.NewRunner(mgr.GitBin(), append(mgr.GitIdentityEnv(nil),
		"GIT_AUTHOR_NAME=Dana Rivera", "GIT_AUTHOR_EMAIL=dana@example.com",
		"GIT_COMMITTER_NAME=Dana Rivera", "GIT_COMMITTER_EMAIL=dana@example.com"))
	run := func(dir string, args ...string) error {
		if _, stderr, err := git.Run(ctx, dir, args...); err != nil {
			return fmt.Errorf("git %v: %v: %s", args, err, stderr)
		}
		return nil
	}
	write := func(path, body string) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(body), 0o644)
	}

	// Member repos with a little history.
	api, web := filepath.Join(root, "api"), filepath.Join(root, "web")
	for path, body := range map[string]string{
		filepath.Join(api, "go.mod"):            "module example.com/api\n\ngo 1.26\n",
		filepath.Join(api, "main.go"):           apiMain,
		filepath.Join(api, "handlers/users.go"): apiUsers,
		filepath.Join(api, "README.md"):         "# api\n\nThe Acme public API.\n",
		filepath.Join(web, "package.json"):      "{\n  \"name\": \"web\",\n  \"private\": true\n}\n",
		filepath.Join(web, "src/App.tsx"):       webApp,
		filepath.Join(web, "README.md"):         "# web\n\nThe Acme dashboard.\n",
	} {
		if err := write(path, body); err != nil {
			return err
		}
	}
	for _, dir := range []string{api, web} {
		for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "initial import"}} {
			if err := run(dir, args...); err != nil {
				return err
			}
		}
	}
	// A feature branch to review: rate limiting for the API.
	if err := run(api, "checkout", "-q", "-b", "feat/rate-limit"); err != nil {
		return err
	}
	if err := write(filepath.Join(api, "middleware/ratelimit.go"), apiRateLimit); err != nil {
		return err
	}
	if err := write(filepath.Join(api, "main.go"), apiMainLimited); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "api: token-bucket rate limiting per client"}, {"checkout", "-q", "main"}} {
		if err := run(api, args...); err != nil {
			return err
		}
	}

	// The workspace and its setup state (so the wizard does not auto-run).
	if err := workspace.CreateWith(root, map[string]string{"api": "api", "web": "web"}); err != nil {
		return err
	}
	ws, err := workspace.Load(root)
	if err != nil {
		return err
	}
	if err := setup.SaveState(setup.WorkspaceStatePath(ws.Root), setup.State{Finished: true}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ws.Root, ".dhi", "welcome.seen"), []byte("seen\n"), 0o644); err != nil {
		return err
	}

	// The squad: a lead, a builder, a reviewer and a writer.
	o, err := org.Load(ws.Root)
	if err != nil {
		return err
	}
	tpl, ok := starter.Get("squad")
	if !ok {
		return fmt.Errorf("no squad template")
	}
	if _, err := starter.Apply(ws, o, library.Open(ws), tpl); err != nil {
		return err
	}

	// The board, in every state.
	ts, err := tasks.Open(ws)
	if err != nil {
		return err
	}
	cards := []struct {
		slug, title, who string
		st               tasks.Status
		pri              tasks.Priority
	}{
		{"rate-limit", "Rate-limit the public API", "forge", tasks.InReview, tasks.PriorityHigh},
		{"login-timeout", "Fix login timeout on slow networks", "forge", tasks.Active, tasks.PriorityUrgent},
		{"onboarding-copy", "Rewrite onboarding copy", "quill", tasks.Active, tasks.PriorityNormal},
		{"dark-mode", "Dashboard dark mode", "", tasks.Backlog, tasks.PriorityNormal},
		{"csv-export", "CSV export for reports", "atlas", tasks.Backlog, tasks.PriorityLow},
		{"audit-log", "Audit log for admin actions", "", tasks.Backlog, tasks.PriorityHigh},
		{"api-docs", "Publish API reference docs", "quill", tasks.Done, tasks.PriorityNormal},
		{"ci-cache", "Cache Go modules in CI", "sage", tasks.Done, tasks.PriorityLow},
	}
	for _, c := range cards {
		if err := ts.Create(c.slug, c.title, c.who, ""); err != nil {
			return err
		}
		if err := ts.SetStatus(c.slug, c.st); err != nil {
			return err
		}
		if err := ts.SetPriority(c.slug, c.pri); err != nil {
			return err
		}
	}

	// A lively #general.
	b, err := bus.Open(ws)
	if err != nil {
		return err
	}
	for _, m := range []bus.Message{
		{Channel: "#general", Author: "atlas", Text: "Morning! Plan for today: land rate limiting, then the login timeout fix."},
		{Channel: "#general", Author: bus.Human, Text: "@forge rate limiting first please; marketing launches Thursday."},
		{Channel: "#general", Author: "forge", Text: "On it. Token bucket per client, 100 req/min default, configurable. PR in review within the hour."},
		{Channel: "#general", Author: "sage", Text: "I'll review when it lands. Watch out for clients behind shared NAT."},
		{Channel: "#general", Author: "quill", Text: "Drafting the changelog entry and the docs page for the new limits."},
	} {
		if _, err := b.Post(m); err != nil {
			return err
		}
	}

	// A round-table to shape the launch.
	is, err := ideation.Open(ws)
	if err != nil {
		return err
	}
	if _, err := is.CreateSession(ideation.CreateOptions{Name: "launch-plan", Topic: "Thursday launch: scope, risks, comms",
		Mode: ideation.ModeGroup, Moderator: "atlas", Agents: []string{"atlas", "forge", "sage", "quill"}}); err != nil {
		return err
	}

	// A review of the feature branch with suggestions from sage.
	rs, err := review.Open(ws)
	if err != nil {
		return err
	}
	svc := review.NewService(ws, rs, git, nil)
	wt, err := worktrees.Resolve(ws.Root, "", "")
	if err != nil {
		return err
	}
	rs.SetWorktreeSeam(func(id, member, startpoint string) (string, error) {
		mem, _ := ws.Member(member)
		rel, dst := wt.Review(id, member)
		return rel, git.WorktreeAdd(ctx, mem.Path, dst, "review/"+id, startpoint)
	}, func(string, string) error { return nil })
	r, err := svc.Start(ctx, "api", review.KindBranch, "main", "feat/rate-limit", 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo: review skipped:", err)
	} else {
		_, _ = rs.AddThread(r.ID, review.Thread{File: "middleware/ratelimit.go", Line: 18, Side: review.SideNew,
			Comments: []review.Comment{{Author: "sage", Text: "Clients behind one NAT share an IP; key the bucket on the API token when present.",
				At: time.Now(), Side: review.SideNew, Line: 18, Suggested: true, Severity: "warning"}}})
	}
	fmt.Println("demo workspace ready:", root)
	return nil
}

const apiMain = `package main

import (
	"log"
	"net/http"

	"example.com/api/handlers"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", handlers.GetUser)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
`

const apiMainLimited = `package main

import (
	"log"
	"net/http"
	"time"

	"example.com/api/handlers"
	"example.com/api/middleware"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", handlers.GetUser)

	limit := middleware.RateLimit(100, time.Minute)
	log.Fatal(http.ListenAndServe(":8080", limit(mux)))
}
`

const apiUsers = `package handlers

import (
	"encoding/json"
	"net/http"
)

// GetUser returns one user by id.
func GetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}
`

const apiRateLimit = `package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimit allows n requests per window for each client.
func RateLimit(n int, window time.Duration) func(http.Handler) http.Handler {
	var mu sync.Mutex
	seen := map[string][]time.Time{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			mu.Lock()
			now, hits := time.Now(), seen[ip]
			for len(hits) > 0 && now.Sub(hits[0]) > window {
				hits = hits[1:]
			}
			if len(hits) >= n {
				mu.Unlock()
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			seen[ip] = append(hits, now)
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}
`

const webApp = `export function App() {
  return <main className="dashboard">Acme</main>;
}
`
