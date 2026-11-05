package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/trust"
)

// F-148: :untrust stops the running language servers instead of only
// claiming they are off.
func TestUntrustStopsRunningLanguageServers(t *testing.T) {
	m, st, alpha := trustedEditor(t)
	_, mgr := startFakeServerFor(t, "go", false)
	m.lspMgr = mgr
	if err := st.Trust(alpha); err != nil {
		t.Fatal(err)
	}
	if mgr.ClientFor("go") == nil {
		t.Fatal("no running server to stop")
	}
	msg, _ := m.trustCommand("untrust")
	if mgr.ClientFor("go") != nil || !strings.Contains(msg, "language servers stopped") {
		t.Fatalf("server survived untrust: %q", msg)
	}
}

// fakeJust records that `just` ran.
type fakeJust struct {
	echoTools
	bin string
}

func (f fakeJust) Find(string) (string, bool) { return f.bin, true }

// F-146/F-148: listing tasks in a folder that is not trusted reads the
// justfile; it does not run `just` (which evaluates backticks and imports).
func TestRestrictedTaskListNeverRunsJust(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	bin := filepath.Join(t.TempDir(), "just")
	script := "#!/bin/sh\ntouch " + marker + "\necho 'Available recipes:'\necho '    from-just'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m, alpha := taskEditor(t, fakeJust{bin: bin})
	if err := os.WriteFile(filepath.Join(alpha, "justfile"), []byte("# say hi\nhello:\n\techo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.trust = trust.New(filepath.Join(t.TempDir(), "trust.json"))

	items, _ := m.discoverTasks()
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("just ran in an untrusted folder")
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Title)
	}
	if got := strings.Join(names, ","); !strings.Contains(got, "hello") || strings.Contains(got, "from-just") {
		t.Fatalf("expected the justfile parsed, got %s", got)
	}

	if err := m.trust.Trust(alpha); err != nil {
		t.Fatal(err)
	}
	items, _ = m.discoverTasks()
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("trusted folder should list through just")
	}
	_ = items
}

// F-165: the editor's Shutdown removes the rebase shim.
func TestShutdownClosesRebaseServer(t *testing.T) {
	m, alpha := rebaseEditor(t)
	if _, err := m.rebaseServer(alpha); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	if m.rebase != nil {
		t.Fatal("shim still running after Shutdown")
	}
}
