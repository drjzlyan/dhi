package bus

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func fixtureWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestPostAssignsPersistsFansOut(t *testing.T) {
	ws := fixtureWS(t)
	b, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	ch, cancel := b.Subscribe("#general")
	defer cancel()

	m1, err := b.Post(Message{Channel: "#general", Author: Human, Text: "hello @scout"})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if m1.ID == 0 || m1.At.IsZero() {
		t.Errorf("stamping failed: %+v", m1)
	}
	select {
	case got := <-ch:
		if got.ID != m1.ID || got.Text != "hello @scout" {
			t.Errorf("fanout = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no fanout")
	}

	m2, _ := b.Post(Message{Channel: "#general", Author: "scout", Text: "hi"})
	if m2.ID <= m1.ID {
		t.Errorf("ids not monotonic: %d then %d", m1.ID, m2.ID)
	}

	// Replay from disk restores the id counter.
	b2, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m3, err := b2.Post(Message{Channel: "#general", Author: Human, Text: "again"})
	if err != nil {
		t.Fatal(err)
	}
	if m3.ID <= m2.ID {
		t.Errorf("replayed counter behind: %d not > %d", m3.ID, m2.ID)
	}
}

func TestHistoryAndThreads(t *testing.T) {
	ws := fixtureWS(t)
	b, _ := Open(ws)
	root, _ := b.Post(Message{Channel: "#dev", Author: Human, Text: "root"})
	b.Post(Message{Channel: "#dev", Author: "scout", Thread: root.ID, Text: "in thread"})
	b.Post(Message{Channel: "#dev", Author: "owl", Text: "top level"})

	top := b.History("#dev", 0)
	if len(top) != 2 {
		t.Fatalf("top-level = %d messages", len(top))
	}
	thread := b.History("#dev", root.ID)
	if len(thread) != 1 || thread[0].Author != "scout" {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestDMChannels(t *testing.T) {
	ws := fixtureWS(t)
	b, _ := Open(ws)
	if _, err := b.Post(Message{Channel: "dm:scout", Author: Human, Text: "psst"}); err != nil {
		t.Fatalf("dm post: %v", err)
	}
	got := b.Channels()
	if len(got) != 1 || got[0] != "dm:scout" {
		t.Errorf("channels = %v", got)
	}
	if h := b.History("dm:scout", 0); len(h) != 1 || h[0].Text != "psst" {
		t.Errorf("history = %+v", h)
	}
}

func TestValidation(t *testing.T) {
	ws := fixtureWS(t)
	b, _ := Open(ws)
	for _, bad := range []Message{
		{Channel: "general", Author: Human, Text: "x"},  // missing prefix
		{Channel: "#../evil", Author: Human, Text: "x"}, // traversal
		{Channel: "#ok", Author: "", Text: "x"},         // no author
		{Channel: "#ok", Author: Human, Text: "   "},    // blank text
	} {
		if _, err := b.Post(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestMentions(t *testing.T) {
	got := Mentions("@scout review @owl.primed then @scout again, mail@not.mention")
	want := []string{"scout", "owl.primed"}
	if len(got) != len(want) {
		t.Fatalf("mentions = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mentions[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestThreadOf(t *testing.T) {
	m := Message{ID: 7}
	if ThreadOf(m) != 7 {
		t.Error("top-level thread root should be own id")
	}
	m.Thread = 3
	if ThreadOf(m) != 3 {
		t.Error("thread reply root should be parent id")
	}
}

func TestOpenMissingTreeIsEmpty(t *testing.T) {
	ws := fixtureWS(t)
	os.RemoveAll(filepath.Join(ws.Root, ".dhi", "channels"))
	b, err := Open(ws)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(b.Channels()) != 0 {
		t.Error("expected empty bus")
	}
}

// F-078: one corrupt line no longer wipes a channel — it is quarantined
// at Open and the rest of the history stays readable.
func TestCorruptLineIsQuarantinedNotFatal(t *testing.T) {
	ws := fixtureWS(t)
	b, _ := Open(ws)
	root, _ := b.Post(Message{Channel: "#general", Author: Human, Text: "first"})
	_, _ = b.Post(Message{Channel: "#general", Author: "scout", Thread: root.ID, Text: "reply"})
	p, _ := fileFor(ws.Root, "#general")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("{not json\n")
	_ = f.Close()
	_, _ = b.Post(Message{Channel: "#general", Author: Human, Text: "after"})

	if got := b.History("#general", 0); len(got) != 2 {
		t.Fatalf("history with a corrupt line = %d messages, want 2", len(got))
	}
	b2, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(b2.Repaired()) != 1 {
		t.Fatalf("repaired = %v", b2.Repaired())
	}
	if q, _ := os.ReadFile(p + ".corrupt"); string(q) != "{not json\n" {
		t.Fatalf("quarantine = %q", q)
	}
	// Recent: a bounded tail, thread view includes its root.
	if got := b2.Recent("#general", root.ID, 100); len(got) != 2 || got[0].ID != root.ID {
		t.Fatalf("recent thread = %+v", got)
	}
	if got := b2.Recent("#general", 0, 1); len(got) != 1 || got[0].Text != "after" {
		t.Fatalf("recent window 1 = %+v", got)
	}
}
