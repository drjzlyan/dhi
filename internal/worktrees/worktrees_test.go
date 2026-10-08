package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLayoutIsExactlyTheOldInWorkspaceOne(t *testing.T) {
	l, err := Resolve("/ws/acme", "", "/home/me")
	if err != nil {
		t.Fatal(err)
	}
	if l.External() || l.Base() != "" {
		t.Fatalf("empty setting must keep the defaults: %+v", l)
	}
	rel, abs := l.Task("fix-login", "api")
	if rel != ".dhi/tasks/fix-login/api" || abs != "/ws/acme/.dhi/tasks/fix-login/api" {
		t.Fatalf("task = %q %q", rel, abs)
	}
	rel, abs = l.Review("api-pr-4", "api")
	if rel != ".dhi/reviews/api-pr-4/api" || abs != "/ws/acme/.dhi/reviews/api-pr-4/api" {
		t.Fatalf("review = %q %q", rel, abs)
	}
	if err := l.Ensure(); err != nil {
		t.Fatalf("default Ensure must be a no-op: %v", err)
	}
}

func TestExternalRootsExpandHomeAndRelativePaths(t *testing.T) {
	for _, c := range []struct{ name, setting, prefix string }{
		{"absolute", "/fast/wt", "/fast/wt/acme-"},
		{"home", "~/wt", "/home/me/wt/acme-"},
		{"bare home", "~", "/home/me/acme-"},
		{"relative to the workspace", "../wt", "/ws/wt/acme-"},
		{"messy", "/fast//wt/../wt/", "/fast/wt/acme-"},
	} {
		l, err := Resolve("/ws/acme", c.setting, "/home/me")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !l.External() || !strings.HasPrefix(l.Base(), c.prefix) {
			t.Errorf("%s: base = %q, want prefix %q", c.name, l.Base(), c.prefix)
		}
	}
	if _, err := Resolve("/ws/acme", "~/wt", ""); err == nil {
		t.Error("~ without a home directory was accepted")
	}
}

func TestExternalPathsAreStoredRelativeToTheWorkspaceAndJoinBack(t *testing.T) {
	l, err := Resolve("/ws/acme", "/fast/wt", "")
	if err != nil {
		t.Fatal(err)
	}
	rel, abs := l.Task("fix-login", "api")
	if !strings.HasPrefix(rel, "../../fast/wt/acme-") || !strings.HasSuffix(rel, "/tasks/fix-login/api") {
		t.Fatalf("rel = %q", rel)
	}
	if got := filepath.Join("/ws/acme", rel); got != abs {
		t.Fatalf("a consumer joining rel onto the root gets %q, want %q", got, abs)
	}
	if filepath.Base(rel) != "api" {
		t.Fatal("cleanup derives the member from the last path element")
	}
	_, rabs := l.Review("api-pr-4", "api")
	if !strings.HasSuffix(rabs, "/reviews/api-pr-4/api") {
		t.Fatalf("review abs = %q", rabs)
	}
}

func TestWorkspacesSharingARootNeverCollide(t *testing.T) {
	a, _ := Resolve("/ws/one/acme", "/fast/wt", "")
	b, _ := Resolve("/ws/two/acme", "/fast/wt", "") // same folder name, other place
	if a.Base() == b.Base() {
		t.Fatalf("both workspaces would use %q", a.Base())
	}
	again, _ := Resolve("/ws/one/acme", "/fast/wt", "")
	if again.Base() != a.Base() {
		t.Fatal("the location must be stable across runs")
	}
}

func TestEnsureCreatesTheExternalBase(t *testing.T) {
	root := t.TempDir()
	l, err := Resolve(filepath.Join(root, "ws"), filepath.Join(root, "wt"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Ensure(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(l.Base()); err != nil || !st.IsDir() {
		t.Fatalf("base not created: %v", err)
	}
}
