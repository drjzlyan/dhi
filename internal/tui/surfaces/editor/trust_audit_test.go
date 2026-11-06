package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// `just --list` evaluates the justfile (backticks can take seconds): in a
// trusted folder :task returns at once and opens the picker when the
// listing lands; a second :task meanwhile waits.
func TestTaskListThroughJustRunsOffTheUIGoroutine(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "go")
	bin := filepath.Join(t.TempDir(), "just")
	script := "#!/bin/sh\nwhile [ ! -e " + gate + " ]; do sleep 0.02; done\necho 'Available recipes:'\necho '    from-just'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m, alpha := taskEditor(t, fakeJust{bin: bin})
	if err := os.WriteFile(filepath.Join(alpha, "justfile"), []byte("hello:\n\techo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.trust = trust.New(filepath.Join(t.TempDir(), "trust.json"))
	if err := m.trust.Trust(alpha); err != nil {
		t.Fatal(err)
	}
	if msg, _ := m.taskCommand("task"); msg != "task: looking for tasks…" || m.taskPick != nil {
		t.Fatalf("while just runs: %q picker=%v", msg, m.taskPick != nil)
	}
	if msg, _ := m.taskCommand("task"); !strings.Contains(msg, "still looking") {
		t.Fatalf("second :task: %q", msg)
	}
	_ = os.WriteFile(gate, nil, 0o644)
	for m.tasksLoading {
		select {
		case msg := <-m.termMsgs:
			m.Update(msg)
		case <-time.After(10 * time.Second):
			t.Fatal("the listing never landed")
		}
	}
	if m.taskPick == nil || m.mode != modeTasks {
		t.Fatal("the picker should open when the listing lands")
	}
}
