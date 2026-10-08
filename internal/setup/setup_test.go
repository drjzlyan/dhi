package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestStateRoundTripAndMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", StateFile)
	if s, err := LoadState(p); err != nil || s.Finished || s.Has("x") {
		t.Fatalf("missing = %+v err=%v", s, err)
	}
	s := State{Finished: true}
	s.Mark("identity", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err := SaveState(p, s); err != nil {
		t.Fatal(err)
	}
	back, err := LoadState(p)
	if err != nil || !back.Finished || back.Steps["identity"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("back = %+v err=%v", back, err)
	}
	os.WriteFile(p, []byte("{nope"), 0o644)
	if _, err := LoadState(p); err == nil {
		t.Fatal("corrupt state must error, not reset silently")
	}
}

func TestShouldAutoRun(t *testing.T) {
	cases := []struct {
		name string
		in   AutoRunInput
		want bool
	}{
		{"bare dir, first time", AutoRunInput{}, true},
		{"bare dir, finished before", AutoRunInput{UserFinished: true}, false},
		{"fresh clone of a team workspace", AutoRunInput{HasWorkspace: true}, true},
		{"existing workspace, welcome seen (this repo)", AutoRunInput{HasWorkspace: true, WelcomeSeen: true}, false},
		{"workspace already onboarded", AutoRunInput{HasWorkspace: true, WorkspaceSetup: true}, false},
		{"returning user in a new clone", AutoRunInput{HasWorkspace: true, UserFinished: true}, false},
		{"palette forces it", AutoRunInput{HasWorkspace: true, UserFinished: true, WelcomeSeen: true, ForcedByCommand: true}, true},
	}
	for _, c := range cases {
		if got := ShouldAutoRun(c.in); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func mkGit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverMembers(t *testing.T) {
	root := t.TempDir()
	mkGit(t, filepath.Join(root, "web"))
	mkGit(t, filepath.Join(root, "API Server"))
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	mkGit(t, filepath.Join(root, ".hidden"))
	got := DiscoverMembers(root)
	want := []MemberSpec{{"api-server", "API Server"}, {"web", "web"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("children = %+v want %+v", got, want)
	}

	repo := t.TempDir()
	mkGit(t, repo)
	one := DiscoverMembers(repo)
	if len(one) != 1 || one[0].Path != "." || workspace.ValidateName(one[0].Name) != nil {
		t.Fatalf("repo root = %+v", one)
	}

	bare := t.TempDir()
	if m := DiscoverMembers(bare); len(m) != 1 || m[0].Path != "." {
		t.Fatalf("no repos = %+v", m)
	}
}

func TestInitWorkspaceLoadsAndIgnoresRuntimeState(t *testing.T) {
	root := t.TempDir()
	mkGit(t, filepath.Join(root, "web"))
	members, err := InitWorkspace(root, nil)
	if err != nil || len(members) != 1 {
		t.Fatalf("init = %+v err=%v", members, err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.Member("web"); !ok {
		t.Fatal("web member missing")
	}
	ign, err := os.ReadFile(filepath.Join(root, ".dhi", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"channels/", "tasks/", "agents/*/runs/", "setup.json", "config.toml"} {
		if !strings.Contains(string(ign), must) {
			t.Errorf(".dhi/.gitignore lacks %q", must)
		}
	}
	for _, tracked := range []string{"workspace.toml\n", "conventions.toml\n"} {
		if strings.Contains(string(ign), "\n"+tracked) {
			t.Errorf("contract file %q must not be ignored", tracked)
		}
	}
	if _, err := InitWorkspace(root, nil); err == nil {
		t.Fatal("second init must refuse")
	}
}
