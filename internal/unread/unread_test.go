package unread

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func testWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	return &workspace.Workspace{Root: t.TempDir()}
}

func testBus(t *testing.T, ws *workspace.Workspace) *bus.Bus {
	t.Helper()
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func post(t *testing.T, b *bus.Bus, m bus.Message) bus.Message {
	t.Helper()
	got, err := b.Post(m)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// seedBus posts a general channel (3 top-level, 1 threaded reply) and a
// DM (2 top-level) and returns the IDs.
func seedBus(t *testing.T, b *bus.Bus) (general, dm, threadRoot int64) {
	general = post(t, b, bus.Message{Channel: "#general", Author: "scout", Text: "@you one"}).ID
	post(t, b, bus.Message{Channel: "#general", Author: "scout", Text: "chatter with muse"})
	post(t, b, bus.Message{Channel: "#general", Author: "you", Text: "on it"})
	threadRoot = post(t, b, bus.Message{Channel: "#general", Author: "muse", Text: "deep dive"}).ID
	post(t, b, bus.Message{Channel: "#general", Author: "scout", Text: "@you in the thread", Thread: threadRoot})
	dm = post(t, b, bus.Message{Channel: "dm:scout", Author: "scout", Text: "heads up"}).ID
	post(t, b, bus.Message{Channel: "dm:scout", Author: "scout", Text: "and this"})
	return
}

func TestDecodeStrict(t *testing.T) {
	if _, err := Decode([]byte(`{"schema":1,"channels":{},"extra":true}`)); err == nil ||
		!strings.Contains(err.Error(), `unknown key "extra"`) {
		t.Fatalf("unknown top-level key: %v", err)
	}
	if _, err := Decode([]byte(`{"schema":2,"channels":{}}`)); err == nil ||
		!strings.Contains(err.Error(), "schema 2, want 1") {
		t.Fatalf("bad schema: %v", err)
	}
	if _, err := Decode([]byte(`{"channels":{}}`)); err == nil ||
		!strings.Contains(err.Error(), `missing key "schema"`) {
		t.Fatalf("missing schema: %v", err)
	}
	if _, err := Decode([]byte(`{"schema":1,"channels":{"#general":"x"}}`)); err == nil ||
		!strings.Contains(err.Error(), "channels[#general]") {
		t.Fatalf("non-number watermark: %v", err)
	}
	if _, err := Decode([]byte(`{"schema":1,"snoozes":[{"channel":"dm:a","messageID":1,"until":"soon","nope":1}]}`)); err == nil ||
		!strings.Contains(err.Error(), `unknown key "nope"`) {
		t.Fatalf("unknown snooze key: %v", err)
	}
	if _, err := Decode([]byte(`{"schema":1,"snoozes":[{"channel":"dm:a","messageID":1,"until":"soon"}]}`)); err == nil ||
		!strings.Contains(err.Error(), `until "soon" is not RFC3339`) {
		t.Fatalf("bad until: %v", err)
	}
	if _, err := Decode([]byte(`{"schema":1,"snoozes":[{"messageID":1,"until":"2026-01-01T00:00:00Z"}]}`)); err == nil ||
		!strings.Contains(err.Error(), "channel") {
		t.Fatalf("missing snooze channel: %v", err)
	}

	good := `{"schema":1,"channels":{"#general":7},"snoozes":[{"channel":"dm:a","messageID":3,"until":"2026-01-02T00:00:00Z"}]}`
	d, err := Decode([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if d.Channels["#general"] != 7 || len(d.Snoozes) != 1 || d.Snoozes[0].MessageID != 3 {
		t.Fatalf("decoded = %+v", d)
	}
}

func TestOpenSeedsMissingFile(t *testing.T) {
	ws := testWS(t)
	b := testBus(t, ws)
	seedBus(t, b)

	s, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(ws.Root, File))
	if err != nil {
		t.Fatalf("seed did not write: %v", err)
	}
	d, err := Decode(got)
	if err != nil {
		t.Fatalf("seeded file malformed: %v", err)
	}
	if len(s.Notes()) == 0 || !strings.Contains(s.Notes()[0], "seeded") {
		t.Fatalf("seeding not named: %v", s.Notes())
	}
	// Re-open reads the seeded file (no re-seed note).
	s2, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Notes()) != 0 {
		t.Fatalf("second open notes = %v", s2.Notes())
	}
	if d.Channels["#general"] == 0 || d.Channels["dm:scout"] == 0 {
		t.Fatalf("seed missing watermarks: %+v", d.Channels)
	}
	// Top-level items at/below the watermark are read; the thread reply
	// (ID > top-level max, thread unopened) is still unread.
	items := s.Unread(b, time.Now())
	topGen := b.History("#general", 0)
	lastGen := topGen[len(topGen)-1].ID
	var want []int64
	for _, r := range b.History("#general", threadRootOf(b)) {
		if r.ID > lastGen && AddressedToHuman(r) {
			want = append(want, r.ID)
		}
	}
	var ids []int64
	for _, it := range items {
		ids = append(ids, it.Msg.ID)
	}
	if len(ids) != len(want) {
		t.Fatalf("unread ids = %v, want %v", ids, want)
	}
}

func threadRootOf(b *bus.Bus) int64 {
	for _, m := range b.History("#general", 0) {
		if m.Author == "muse" {
			return m.ID
		}
	}
	return 0
}

func TestMarkReadMonotonic(t *testing.T) {
	ws := testWS(t)
	b := testBus(t, ws)
	seedBus(t, b)
	s, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRead("#general", 5); err != nil {
		t.Fatal(err)
	}
	// Rewind is a no-op.
	if err := s.MarkRead("#general", 2); err != nil {
		t.Fatal(err)
	}
	snap := s.data
	if snap.Channels["#general"] != 5 {
		t.Fatalf("watermark = %d, want 5 (no rewind)", snap.Channels["#general"])
	}
	if err := s.MarkRead("#general", 9); err != nil {
		t.Fatal(err)
	}
	if s.data.Channels["#general"] != 9 {
		t.Fatal("advance failed")
	}
	// Round-trips through disk.
	s2, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	if s2.data.Channels["#general"] != 9 {
		t.Fatalf("round-trip watermark = %d, want 9", s2.data.Channels["#general"])
	}
}

func TestSnoozeRoundTripAndExpiry(t *testing.T) {
	ws := testWS(t)
	b := testBus(t, ws)
	seedBus(t, b)
	s, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	// Post a fresh DM so it sits ABOVE the seeded watermark (unread).
	fresh := post(t, b, bus.Message{Channel: "dm:scout", Author: "scout", Text: "new note"})
	until := time.Now().Add(time.Hour)
	if err := s.Snooze("dm:scout", fresh.ID, until); err != nil {
		t.Fatal(err)
	}
	snap := s.Unread(b, time.Now())
	found := false
	for _, it := range snap {
		if it.Msg.ID == fresh.ID && !it.Snoozed.IsZero() {
			found = true
		}
	}
	if !found {
		t.Fatalf("snoozed item not flagged: %+v", snap)
	}
	// Replace, don't duplicate.
	if err := s.Snooze("dm:scout", fresh.ID, until.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := len(s.data.Snoozes); n != 1 {
		t.Fatalf("snoozes = %d, want 1 (replace)", n)
	}
	// Unsnooze.
	if err := s.Unsnooze("dm:scout", fresh.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(s.data.Snoozes); n != 0 {
		t.Fatalf("snoozes = %d, want 0", n)
	}
	// Expired snoozes are dropped on the next open.
	if err := s.Snooze("dm:scout", fresh.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s2.data.Snoozes); n != 0 {
		t.Fatalf("expired snooze kept: %+v", s2.data.Snoozes)
	}
	if len(s2.Notes()) == 0 || !strings.Contains(s2.Notes()[0], "expired") {
		t.Fatalf("expiry not named: %v", s2.Notes())
	}
}

func TestPruneAbsentChannels(t *testing.T) {
	ws := testWS(t)
	b := testBus(t, ws)
	seedBus(t, b)
	s, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	// Invent state for a channel that does not exist on the bus.
	s.data.Channels["#ghost"] = 3
	s.data.Channels[ThreadScope("#ghost", 1)] = 4
	s.data.Snoozes = append(s.data.Snoozes,
		Snooze{Channel: "#ghost", MessageID: 9, Until: time.Now().Add(time.Hour)})
	if err := s.write(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.data.Channels["#ghost"]; ok {
		t.Fatal("absent-channel watermark not pruned")
	}
	if _, ok := s2.data.Channels[ThreadScope("#ghost", 1)]; ok {
		t.Fatal("absent-thread watermark not pruned")
	}
	if len(s2.data.Snoozes) != 0 {
		t.Fatal("absent-channel snooze not dropped")
	}
}

func TestSubscribeFiresOnWrite(t *testing.T) {
	ws := testWS(t)
	b := testBus(t, ws)
	seedBus(t, b)
	s, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	ch, cancel := s.Subscribe()
	defer cancel()
	go func() { _ = s.MarkRead("#general", 99) }()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no change signal on MarkRead")
	}
}

func TestAddressedToHuman(t *testing.T) {
	cases := []struct {
		name string
		m    bus.Message
		want bool
	}{
		{"dm agent", bus.Message{Channel: "dm:scout", Author: "scout", Text: "hi"}, true},
		{"dm you", bus.Message{Channel: "dm:scout", Author: "you", Text: "hi"}, false},
		{"channel mention you", bus.Message{Channel: "#g", Author: "scout", Text: "hi @you pls"}, true},
		{"channel agent-to-agent", bus.Message{Channel: "#g", Author: "scout", Text: "hi @muse"}, false},
		{"channel chatter", bus.Message{Channel: "#g", Author: "scout", Text: "deployed"}, false},
		{"channel you mentioning agent", bus.Message{Channel: "#g", Author: "you", Text: "thanks @scout"}, false},
	}
	for _, c := range cases {
		if got := AddressedToHuman(c.m); got != c.want {
			t.Fatalf("%s: AddressedToHuman = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestScanOrderAndThreadRules(t *testing.T) {
	ws := testWS(t)
	b := testBus(t, ws)
	seedBus(t, b)
	now := time.Now()

	// Empty watermarks: every addressed-to-human message counts, oldest
	// first. Includes the thread reply (unopened thread counts fully).
	items := Scan(map[string]int64{}, nil, b, now)
	var ids []int64
	for _, it := range items {
		ids = append(ids, it.Msg.ID)
	}
	wantIDs := []int64{}
	for _, ch := range b.Channels() {
		for _, m := range b.History(ch, 0) {
			if AddressedToHuman(m) {
				wantIDs = append(wantIDs, m.ID)
			}
			for _, r := range b.History(ch, m.ID) {
				if AddressedToHuman(r) {
					wantIDs = append(wantIDs, r.ID)
				}
			}
		}
	}
	if len(ids) != len(wantIDs) {
		t.Fatalf("ids = %v, want %v", ids, wantIDs)
	}
	for i := range ids {
		if ids[i] != wantIDs[i] {
			t.Fatalf("ids = %v, want %v (oldest first)", ids, wantIDs)
		}
	}

	// Watermark past the top of #general: only the DM + thread reply
	// (thread unopened → still counts).
	generalTop := b.History("#general", 0)
	lastGen := generalTop[len(generalTop)-1].ID
	items = Scan(map[string]int64{"#general": lastGen}, nil, b, now)
	for _, it := range items {
		if it.Msg.Channel == "#general" && it.Msg.Thread == 0 {
			t.Fatalf("watermarked top-level leaked: %+v", it.Msg)
		}
	}

	// Open the thread (watermark = reply ID): the thread reply drops.
	tr := threadRootOf(b)
	replies := b.History("#general", tr)
	items = Scan(map[string]int64{"#general": lastGen, ThreadScope("#general", tr): replies[len(replies)-1].ID}, nil, b, now)
	for _, it := range items {
		if it.Msg.Thread != 0 {
			t.Fatalf("opened thread reply leaked: %+v", it.Msg)
		}
	}
}

func TestThreadScopeReadsTopLevel(t *testing.T) {
	ws := testWS(t)
	b := testBus(t, ws)
	seedBus(t, b)
	now := time.Now()

	// Open ONLY the "deep dive" thread: its root (a top-level message
	// mentioning nothing) was read via the thread scope — but a
	// top-level mention elsewhere stays unread.
	tr := threadRootOf(b)
	topGen := b.History("#general", 0)
	lastGen := topGen[len(topGen)-1].ID
	replies := b.History("#general", tr)
	ch := map[string]int64{ThreadScope("#general", tr): replies[len(replies)-1].ID}
	items := Scan(ch, nil, b, now)
	sawMention := false
	for _, it := range items {
		if it.Msg.ID == tr {
			t.Fatalf("thread root read via thread scope leaked: %+v", it.Msg)
		}
		if it.Msg.Channel == "#general" && it.Msg.Thread == 0 {
			if it.Msg.ID == 1 {
				sawMention = true // unopened top-level mention survives
			} else {
				t.Fatalf("unexpected top-level item: %+v", it.Msg)
			}
		}
	}
	if !sawMention {
		t.Fatal("unopened top-level mention dropped")
	}
	_ = lastGen

	// Counts groups per channel, snoozed included. dm:scout has no
	// watermark in ch → all three of its agent messages count.
	until := now.Add(time.Hour)
	dm := post(t, b, bus.Message{Channel: "dm:scout", Author: "scout", Text: "fresh"})
	counts := Counts(ch, []Snooze{{Channel: "dm:scout", MessageID: dm.ID, Until: until}}, b, now)
	if counts["dm:scout"] != 3 {
		t.Fatalf("dm count = %d, want 3 (snoozed included)", counts["dm:scout"])
	}
	if counts["#general"] < 1 {
		t.Fatalf("general count = %d, want >= 1", counts["#general"])
	}
}

func TestMarkChannelReadBulk(t *testing.T) {
	ws := testWS(t)
	if err := os.MkdirAll(filepath.Join(ws.Root, workspace.DHIDir), 0o755); err != nil {
		t.Fatal(err)
	}
	b := testBus(t, ws)
	s, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	seedBus(t, b) // posts after seed → all unread
	if items := s.Unread(b, time.Now()); len(items) == 0 {
		t.Fatal("expected unread seed messages")
	}
	if err := s.MarkChannelRead("#general", b); err != nil {
		t.Fatal(err)
	}
	// #general (top-level + thread) is read; the DM is untouched.
	for _, it := range s.Unread(b, time.Now()) {
		if it.Msg.Channel == "#general" {
			t.Fatalf("#general still unread: %+v", it)
		}
	}
	if items := s.Unread(b, time.Now()); len(items) == 0 {
		t.Fatal("DM should remain unread")
	}
	// Reload proves it persisted.
	s2, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range s2.Unread(b, time.Now()) {
		if it.Msg.Channel == "#general" {
			t.Fatalf("#general still unread after reload: %+v", it)
		}
	}
}

func TestMarkAllReadBulk(t *testing.T) {
	ws := testWS(t)
	if err := os.MkdirAll(filepath.Join(ws.Root, workspace.DHIDir), 0o755); err != nil {
		t.Fatal(err)
	}
	b := testBus(t, ws)
	s, err := Open(ws, b)
	if err != nil {
		t.Fatal(err)
	}
	seedBus(t, b)
	if err := s.MarkAllRead(b); err != nil {
		t.Fatal(err)
	}
	if items := s.Unread(b, time.Now()); len(items) != 0 {
		t.Fatalf("MarkAllRead left %d item(s): %+v", len(items), items)
	}
}
