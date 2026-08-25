package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func setupStore(t *testing.T) (*Store, *workspace.Workspace) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	return s, ws
}

func TestCreatePersistReload(t *testing.T) {
	s, ws := setupStore(t)
	if err := s.Create("fix-login", "Fix login race", "alice", "frontend"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	reloaded, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get("fix-login")
	if !ok || got.Title != "Fix login race" || got.Status != Backlog ||
		got.Assignee != "alice" || got.Team != "frontend" {
		t.Fatalf("reloaded = %+v (found=%v)", got, ok)
	}
	if reloaded.Warnings() != nil {
		t.Errorf("unexpected warnings: %v", reloaded.Warnings())
	}
}

func TestValidation(t *testing.T) {
	s, _ := setupStore(t)
	cases := []struct{ slug, title, assignee, wantErr string }{
		{"Bad Slug", "x", "", "bad slug"},
		{"ok", "  ", "", "title required"},
		{"ok", "t", "Not Valid", "bad assignee"},
		{"you-alias", "t", "you", ""}, // legal
	}
	for _, tc := range cases {
		err := s.Create(tc.slug, tc.title, tc.assignee, "")
		if tc.wantErr == "" && err != nil {
			t.Errorf("Create(%q): %v", tc.slug, err)
			continue
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("Create(%q) = %v, want ~%q", tc.slug, err, tc.wantErr)
		}
	}
	if err := s.Create("you-alias", "dup", "", ""); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Errorf("dup create = %v", err)
	}
}

func TestStatusFlowAndListOrdering(t *testing.T) {
	s, _ := setupStore(t)
	for _, slug := range []string{"b-task", "a-task"} {
		if err := s.Create(slug, "T "+slug, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetStatus("a-task", Active); err != nil {
		t.Fatal(err)
	}
	list := s.List()
	// Flow order groups by column: backlog before active.
	if list[0].Slug != "b-task" || list[1].Slug != "a-task" ||
		list[1].Status != Active {
		t.Fatalf("order = %+v", list)
	}
	if err := s.SetStatus("a-task", "archived"); err == nil ||
		!strings.Contains(err.Error(), "bad status") {
		t.Errorf("invalid status = %v", err)
	}
	if err := s.SetStatus("ghost", Done); err == nil ||
		!strings.Contains(err.Error(), "unknown task") {
		t.Errorf("unknown slug = %v", err)
	}
}

func TestAssignAndTasksOf(t *testing.T) {
	s, _ := setupStore(t)
	s.Create("one", "One", "alice", "")
	s.Create("two", "Two", "alice", "")
	s.Create("three", "Three", "bob", "")
	s.SetStatus("two", Done)

	open := s.TasksOf("alice")
	if len(open) != 1 || open[0].Slug != "one" {
		t.Fatalf("open for alice = %+v (done excluded)", open)
	}
	if err := s.Assign("one", "Bad!"); err == nil {
		t.Error("bad assignee accepted")
	}
}

func TestAttachDetachWithFakeSeam(t *testing.T) {
	s, ws := setupStore(t)
	var detached [][2]string
	s.SetAttach(
		func(slug, member, branch, startpoint string) (string, error) {
			rel := Dir + "/" + slug + "/" + member
			if err := os.MkdirAll(filepath.Join(ws.Root, rel), 0o755); err != nil {
				return "", err
			}
			return rel, nil
		},
		func(slug, relPath string) error {
			detached = append(detached, [2]string{slug, relPath})
			return os.RemoveAll(filepath.Join(ws.Root, relPath))
		},
	)

	s.Create("fix-login", "Fix login race", "alice", "")
	if err := s.Attach("fix-login", "main", "task/fix-login", ""); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	tk, _ := s.Get("fix-login")
	if len(tk.ChangeSets) != 1 || tk.ChangeSets[0].Member != "main" ||
		tk.ChangeSets[0].Branch != "task/fix-login" {
		t.Fatalf("changesets = %+v", tk.ChangeSets)
	}
	if info, err := os.Stat(filepath.Join(ws.Root, tk.ChangeSets[0].Path)); err != nil || !info.IsDir() {
		t.Fatalf("fake worktree missing: %v", err)
	}

	// Re-attach same member replaces the record.
	if err := s.Attach("fix-login", "main", "task/other", ""); err != nil {
		t.Fatal(err)
	}
	tk, _ = s.Get("fix-login")
	if len(tk.ChangeSets) != 1 || tk.ChangeSets[0].Branch != "task/other" {
		t.Fatalf("re-attach = %+v", tk.ChangeSets)
	}

	if err := s.Detach("fix-login", "main"); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if len(detached) != 1 || detached[0][1] != tk.ChangeSets[0].Path {
		t.Fatalf("detach calls = %v", detached)
	}
	if tk, _ = s.Get("fix-login"); len(tk.ChangeSets) != 0 {
		t.Fatalf("record kept after detach: %+v", tk.ChangeSets)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, Dir, "fix-login", "main")); !os.IsNotExist(err) {
		t.Error("worktree dir survived detach")
	}
	if err := s.Detach("fix-login", "main"); err == nil ||
		!strings.Contains(err.Error(), "no changeset") {
		t.Errorf("double detach = %v", err)
	}
}

func TestAttachWithoutSeamFailsVisibly(t *testing.T) {
	s, _ := setupStore(t)
	s.Create("t1", "T", "", "")
	err := s.Attach("t1", "main", "b", "")
	if err == nil || !strings.Contains(err.Error(), "seam unavailable") {
		t.Fatalf("nil seam attach = %v", err)
	}
}

func TestBindThreadValidation(t *testing.T) {
	s, _ := setupStore(t)
	s.Create("t1", "T", "", "")
	if err := s.BindThread("t1", "#general", 7); err != nil {
		t.Fatalf("BindThread: %v", err)
	}
	if got, _ := s.Get("t1"); got.ThreadChannel != "#general" || got.ThreadID != 7 {
		t.Fatalf("binding lost: %+v", got)
	}
	if err := s.BindThread("t1", "not-a-channel", 7); err == nil {
		t.Error("bad channel accepted")
	}
	if err := s.BindThread("ghost", "#general", 1); err == nil {
		t.Error("unknown task bound")
	}
}

func TestRemoveKeepsWorktreesOnDisk(t *testing.T) {
	s, ws := setupStore(t)
	s.SetAttach(
		func(slug, member, branch, sp string) (string, error) {
			rel := Dir + "/" + slug + "/" + member
			os.MkdirAll(filepath.Join(ws.Root, rel), 0o755)
			return rel, nil
		}, nil)
	s.Create("gone", "G", "", "")
	s.Attach("gone", "main", "task/gone", "")

	if err := s.Remove("gone"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("gone"); ok {
		t.Fatal("card survived remove")
	}
	if _, err := os.Stat(filepath.Join(ws.Root, Dir, "gone")); err != nil {
		t.Error("worktree tree was deleted by card removal")
	}
	if _, err := os.Stat(s.cardPathForTest("gone")); !os.IsNotExist(err) {
		t.Error("card file survived")
	}
}

func TestMalformedCardsBecomeWarnings(t *testing.T) {
	_, ws := setupStore(t)
	broken := filepath.Join(ws.Root, Dir, "broken.toml")
	os.MkdirAll(filepath.Dir(broken), 0o755)
	os.WriteFile(broken, []byte("schema = 99\ntitle=\"x\"\n"), 0o644)
	os.WriteFile(filepath.Join(ws.Root, Dir, "notes.txt"), []byte("ignore"), 0o644)

	reloaded, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if w := reloaded.Warnings(); len(w) != 1 || !strings.Contains(w[0], "broken") {
		t.Fatalf("warnings = %v", w)
	}
	if len(reloaded.List()) != 0 {
		t.Fatal("broken card parsed anyway")
	}
}

func TestSubscribePings(t *testing.T) {
	s, _ := setupStore(t)
	ch, cancel := s.Subscribe()
	defer cancel()
	s.Create("pinged", "P", "", "")
	select {
	case c := <-ch:
		if c.Kind != TaskCreated || c.Slug != "pinged" {
			t.Fatalf("event = %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestConcurrentMutations(t *testing.T) {
	s, _ := setupStore(t)
	s.Create("base", "B", "", "")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.SetStatus("base", Statuses[i%len(Statuses)])
			_ = s.List()
			_, _ = s.Get("base")
		}(i)
	}
	wg.Wait()
}
