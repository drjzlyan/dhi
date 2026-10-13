package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestTraceFiltersSearchesAndExports(t *testing.T) {
	m, ws := newSurface(t)
	m.taskStore = seededRunStore(t, ws)
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.bus = b
	root, _ := b.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@alice fix login"})
	_, _ = b.Post(bus.Message{Channel: "#general", Thread: root.ID, Author: "alice", Kind: bus.KindStream, Text: "retrying (attempt 1/3) in 30s… (crash)"})
	_, _ = b.Post(bus.Message{Channel: "#general", Thread: root.ID, Author: "alice", Kind: bus.KindReply, Text: "Fixed the race."})
	if err := m.taskStore.BindThread("fix-login", "#general", root.ID); err != nil {
		t.Fatal(err)
	}
	m.sec = secBoard
	m.HandleKey("r")
	plain := ansi.Strip(m.View())
	for _, want := range []string{"@alice fix login", "retrying (attempt 1/3)", "Fixed the race.", "❯ git status"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("trace missing %q:\n%s", want, plain)
		}
	}
	m.HandleKey("f") // conversation
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "filter conversation") || strings.Contains(plain, "git status") || !strings.Contains(plain, "Fixed the race.") {
		t.Fatalf("conversation filter wrong:\n%s", plain)
	}
	m.HandleKey("f") // tools
	m.HandleKey("/")
	for _, k := range []string{"r", "e", "t", "r", "y"} {
		m.HandleKey(k)
	}
	m.HandleKey("enter")
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "retrying") || strings.Contains(plain, "inspecting") {
		t.Fatalf("search wrong:\n%s", plain)
	}
	for _, tk := range []theme.Tokens{theme.Dark(), theme.Light(), theme.HighContrast()} {
		theme.SwapForTest(t, tk)
		golden.AssertBgIntegrity(t, "trace "+tk.Name, m.View())
	}
	m.HandleKey("x")
	if !strings.HasPrefix(m.trace.note, "exported .dhi/traces/fix-login-") {
		t.Fatalf("export note = %q", m.trace.note)
	}
	files, _ := filepath.Glob(filepath.Join(ws.Root, ".dhi", "traces", "fix-login-*"))
	if len(files) != 2 {
		t.Fatalf("exported files = %v", files)
	}
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if !strings.Contains(string(data), "retrying") {
			t.Fatalf("%s lacks the filtered events:\n%s", f, data)
		}
	}
}
