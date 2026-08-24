package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAddMemberPersistsAndNotifies(t *testing.T) {
	ws, _, _ := setupWorkspace(t)
	gamma := filepath.Join(ws.Root, "gamma")
	if err := os.MkdirAll(gamma, 0o755); err != nil {
		t.Fatal(err)
	}

	ch, cancel := ws.Subscribe()
	defer cancel()

	if err := ws.AddMember("gamma", gamma); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	m, ok := ws.Member("gamma")
	if !ok || m.Path != filepath.Clean(gamma) {
		t.Fatalf("member after add = %+v (found=%v)", m, ok)
	}
	// Roster stays sorted by name.
	snap := ws.Members()
	if snap[0].Name != "alpha" || snap[1].Name != "beta" || snap[2].Name != "gamma" {
		t.Errorf("order = %v", snap)
	}
	select {
	case c := <-ch:
		if c.Kind != Added || c.Name != "gamma" {
			t.Errorf("event = %+v", c)
		}
	default:
		t.Error("no Added event delivered")
	}

	// Reload sees the persisted roster.
	reloaded, err := Load(ws.Root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Member("gamma"); !ok {
		t.Error("add did not persist")
	}
}

func TestAddMemberValidation(t *testing.T) {
	ws, repoA, _ := setupWorkspace(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path, wantErr string
	}{
		{"alpha", outside, "already exists"},             // dup name
		{"delta", repoA, "already registered"},           // dup path
		{"Bad", outside, "bad member name"},              // naming rule
		{"9start", outside, ""},                          // legal: starts with digit
		{"ghost", filepath.Join(ws.Root, "missing"), ""}, // path errors below
	}
	for _, tc := range cases {
		err := ws.AddMember(tc.name, tc.path)
		if tc.wantErr == "" && tc.name == "ghost" {
			if err == nil {
				t.Errorf("ghost path accepted")
			}
			continue
		}
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("AddMember(%q,%q): %v", tc.name, tc.path, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("AddMember(%q) = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestRemoveMemberKeepsTreeAndInvariant(t *testing.T) {
	ws, repoA, _ := setupWorkspace(t)
	if err := ws.RemoveMember("beta"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if _, ok := ws.Member("beta"); ok {
		t.Fatal("beta still registered")
	}
	// Working tree untouched by design.
	if info, err := os.Stat(filepath.Join(repoA)); err != nil || !info.IsDir() {
		t.Errorf("alpha tree disturbed: %v", err)
	}
	if _, err := Load(ws.Root); err != nil {
		t.Fatalf("reload after remove: %v", err)
	}
	if err := ws.RemoveMember("alpha"); err == nil || !strings.Contains(err.Error(), "last member") {
		t.Errorf("last-member removal = %v", err)
	}
	if err := ws.RemoveMember("beta"); err == nil || !strings.Contains(err.Error(), "unknown member") {
		t.Errorf("unknown removal = %v", err)
	}
}

func TestRenameMemberReordersAndPersists(t *testing.T) {
	ws, _, _ := setupWorkspace(t)
	if err := ws.RenameMember("beta", "aab"); err != nil {
		t.Fatalf("RenameMember: %v", err)
	}
	snap := ws.Members()
	if snap[0].Name != "aab" || snap[1].Name != "alpha" {
		t.Errorf("order after rename = %+v", snap)
	}
	if _, ok := ws.Member("beta"); ok {
		t.Error("old alias still registered")
	}
	reloaded, err := Load(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Member("aab"); !ok {
		t.Error("rename not persisted")
	}
	if err := ws.RenameMember("alpha", "aab"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("dup rename = %v", err)
	}
	if err := ws.RenameMember("nope", "x"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("unknown rename = %v", err)
	}
}

func TestSaveWritesRelativePathsUnderRoot(t *testing.T) {
	ws, _, _ := setupWorkspace(t)
	if err := ws.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(ws.Root, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(data)
	if !filepath.IsAbs(filepath.Clean(repoPath(t, ws))) &&
		!strings.Contains(cfg, `"repos/alpha"`) && !strings.Contains(cfg, "repos/alpha") {
		t.Errorf("config lost relative path under root:\n%s", cfg)
	}
	if strings.Contains(cfg, string(filepath.Separator)+".dhi") {
		t.Errorf("config leaked absolute .dhi paths:\n%s", cfg)
	}
}

func repoPath(t *testing.T, ws *Workspace) string {
	t.Helper()
	m, _ := ws.Member("alpha")
	return m.Path
}

func TestConcurrentReadersDuringMutations(t *testing.T) {
	ws, _, _ := setupWorkspace(t)
	names := []string{"c1", "c2", "c3", "c4", "c5"}
	for _, n := range names {
		dir := filepath.Join(ws.Root, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() { // resolver reader
		defer wg.Done()
		vp, _ := ParseVPath("alpha/x/y")
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = ws.Resolve(vp)
				_ = ws.Members()
			}
		}
	}()
	wg.Add(1)
	go func() { // mutator
		defer wg.Done()
		for _, n := range names {
			if err := ws.AddMember(n, filepath.Join(ws.Root, n)); err != nil {
				t.Errorf("add %s: %v", n, err)
				return
			}
		}
	}()
	wg.Add(1)
	go func() { // subscriber
		defer wg.Done()
		ch, cancel := ws.Subscribe()
		defer cancel()
		for {
			select {
			case <-stop:
				return
			case <-ch:
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
	if len(ws.Members()) != 2+len(names) {
		t.Errorf("final roster = %d members", len(ws.Members()))
	}
}
