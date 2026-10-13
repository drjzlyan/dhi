package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-086: a chat turn (no card) asks for its own branch; DHI makes the
// card, binds the thread, provisions the worktree and asks the turn to
// continue there in a fresh run.
func TestWorkBranchGivesAChatTurnACardAndWorktree(t *testing.T) {
	h := episodeHarness(t)
	st, err := tasks.Open(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	st.SetAttach(func(slug, member, branch, _ string) (string, error) {
		return ".dhi/tasks/" + slug + "/" + member, nil
	}, nil)
	h.rt.cfg.Tasks = st
	h.rt.cfg.Provision = func(slug, member string) error { return st.Attach(slug, member, "task/"+slug, "") }
	trig := mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "fix the pipeline"})

	rl := &relaunch{}
	wb := h.rt.workBranchSeam("scout", trig, rl)
	out, err := wb(context.Background(), "fix-pipeline", "Fix the pipeline", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "branch task/fix-pipeline") || !strings.Contains(out, "End this run") {
		t.Fatalf("result = %q", out)
	}
	card, ok := h.rt.boundTask(trig)
	if !ok || card.Slug != "fix-pipeline" || card.Assignee != "scout" || len(card.ChangeSets) != 1 {
		t.Fatalf("bound card = %+v ok=%v", card, ok)
	}
	if rl.get() == "" {
		t.Fatal("no relaunch requested")
	}
	if got := h.rt.cliWorkdir(trig); !strings.HasSuffix(got, ".dhi/tasks/fix-pipeline/api") {
		t.Fatalf("next spawn workdir = %s", got)
	}
	// Asking again on the same thread reports the worktree, no new card.
	again, err := wb(context.Background(), "other", "Other", "", false)
	if err != nil || !strings.Contains(again, "already works in") {
		t.Fatalf("second call = %q %v", again, err)
	}
}

// Another agent's card is not taken over by slug.
func TestWorkBranchRefusesAnotherAgentsCard(t *testing.T) {
	h := episodeHarness(t)
	st, _ := tasks.Open(h.ws)
	h.rt.cfg.Tasks = st
	h.rt.cfg.Provision = func(string, string) error { return nil }
	if err := st.Create("csv", "CSV", "dev", ""); err != nil {
		t.Fatal(err)
	}
	trig := mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "go"})
	_, err := h.rt.workBranchSeam("scout", trig, &relaunch{})(context.Background(), "csv", "CSV", "", false)
	if err == nil || !strings.Contains(err.Error(), "belongs to dev") {
		t.Fatalf("err = %v", err)
	}
}

// The continuation is a visible DHI post that wakes the agent in the
// same thread.
func TestContinueTurnPostsToTheAgent(t *testing.T) {
	h := episodeHarness(t)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout hi"})
	h.rt.continueTurn(context.Background(), "scout", trig, "task x now works in y")
	h.rt.WaitBackground()
	var found bool
	for _, m := range h.bus.History("#general", trig.ID) {
		if m.Author == systemAuthor && strings.HasPrefix(m.Text, "@scout continue: task x") && m.Thread == trig.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no continuation post: %+v", h.bus.History("#general", 0))
	}
}

// M32 review: work_branch carries only what changed during the turn. A
// file you (or another agent) had already changed stays in the shared
// checkout, and the agent is told so.
func TestWorkBranchCarriesOnlyThisTurnsChanges(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	h := episodeHarness(t)
	repo := h.ws.Members()[0].Path
	g := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command(gitBin, append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "a.go"), "package a\n")
	write(filepath.Join(repo, "b.go"), "package a\n")
	g(repo, "init", "-q", "-b", "main")
	g(repo, "add", "a.go", "b.go")
	g(repo, "commit", "-qm", "seed")
	write(filepath.Join(repo, "b.go"), "package a // yours, half done\n") // dirty before the turn

	h.rt.cfg.Git = gitcore.NewRunner(gitBin, os.Environ())
	st, _ := tasks.Open(h.ws)
	st.SetAttach(func(slug, member, branch, _ string) (string, error) {
		rel := filepath.Join(".dhi", "tasks", slug, member)
		g(repo, "worktree", "add", "-q", "-b", branch, filepath.Join(h.ws.Root, rel))
		return rel, nil
	}, nil)
	h.rt.cfg.Tasks = st
	h.rt.cfg.Provision = func(slug, member string) error { return st.Attach(slug, member, "task/"+slug, "") }
	trig := mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "fix it"})
	wb := h.rt.workBranchSeam("scout", trig, &relaunch{}) // the turn begins: b.go is already dirty

	write(filepath.Join(repo, "a.go"), "package a // the agent's fix\n")
	write(filepath.Join(repo, "c.go"), "package a // new\n")
	out, err := wb(context.Background(), "fix", "Fix", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "b.go") {
		t.Fatalf("not told about b.go: %q", out)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if !strings.Contains(read(filepath.Join(repo, "b.go")), "yours") {
		t.Fatal("your half-done b.go left the shared checkout")
	}
	if strings.Contains(read(filepath.Join(repo, "a.go")), "agent") {
		t.Fatal("the agent's a.go edit stayed in the shared checkout")
	}
	card, _ := st.Get("fix")
	wt := filepath.Join(h.ws.Root, card.ChangeSets[0].Path)
	if !strings.Contains(read(filepath.Join(wt, "a.go")), "agent") || read(filepath.Join(wt, "c.go")) == "" {
		t.Fatal("the agent's changes did not reach the worktree")
	}
	if strings.Contains(read(filepath.Join(wt, "b.go")), "yours") {
		t.Fatal("your edit was carried into the agent's branch")
	}
}
