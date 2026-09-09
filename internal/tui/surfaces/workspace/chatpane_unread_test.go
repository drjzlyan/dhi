package workspace

import (
	"strconv"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/unread"
)

// paneWithStore opens the read-mark store BEFORE any message lands, so
// seeded channels have empty watermarks (fresh-install semantics) and
// every subsequent post is genuinely unread.
func paneWithStore(t *testing.T) (*chatPane, *unread.Store) {
	t.Helper()
	p, ws, _, _ := newPaneFixture(t)
	us, err := unread.Open(ws, p.bus)
	if err != nil {
		t.Fatal(err)
	}
	return p, us
}

func TestSwitchChannelMarksRead(t *testing.T) {
	p, us := paneWithStore(t)
	posted, err := p.bus.Post(bus.Message{Channel: "#frontend", Author: "scout",
		Text: "@you ping"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	p.onRead = func(scope string, id int64) {
		got = append(got, scope+"="+strconv.FormatInt(id, 10))
		_ = us.MarkRead(scope, id)
	}
	p.switchChannel(1) // #general → #frontend
	if len(got) != 1 || got[0] != "#frontend="+strconv.FormatInt(posted.ID, 10) {
		t.Fatalf("onRead = %v, want [#frontend=%d]", got, posted.ID)
	}
	// The store's watermark advanced: the mention no longer surfaces.
	if items := us.Unread(p.bus, time.Now()); len(items) != 0 {
		t.Fatalf("unread after switch = %+v", items)
	}
}

func TestThreadOpenMarksThreadScope(t *testing.T) {
	p, us := paneWithStore(t)
	root, err := p.bus.Post(bus.Message{Channel: "#general", Author: "muse", Text: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "@you look here", Thread: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	p.onRead = func(scope string, id int64) {
		got = append(got, scope+"="+strconv.FormatInt(id, 10))
		_ = us.MarkRead(scope, id)
	}
	// cursor sits on the first top-level message (the root); open thread.
	if !p.handleKey("t") {
		t.Fatal("t not consumed")
	}
	want := unread.ThreadScope("#general", root.ID) + "=" + strconv.FormatInt(reply.ID, 10)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("onRead = %v, want [%s]", got, want)
	}
	if items := us.Unread(p.bus, time.Now()); len(items) != 0 {
		t.Fatalf("thread reply still unread: %+v", items)
	}
}

func TestPostMarksChannelRead(t *testing.T) {
	p, us := paneWithStore(t)
	if _, err := p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "@you ping"}); err != nil {
		t.Fatal(err)
	}
	p.onRead = func(scope string, id int64) { _ = us.MarkRead(scope, id) }
	p.post("on it") // the human replies → channel read
	if items := us.Unread(p.bus, time.Now()); len(items) != 0 {
		t.Fatalf("posting did not read the channel: %+v", items)
	}
}

func TestOpenAtMarksThreadReadAndClearsRow(t *testing.T) {
	p, us := paneWithStore(t)
	mention, err := p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "@you decide please"})
	if err != nil {
		t.Fatal(err)
	}
	if items := us.Unread(p.bus, time.Now()); len(items) != 1 {
		t.Fatalf("precondition: %d unread, want 1", len(items))
	}
	p.onRead = func(scope string, id int64) { _ = us.MarkRead(scope, id) } // the Model seam
	if !p.openAt("#general", mention.ID, mention.ID) {
		t.Fatal("openAt failed")
	}
	if items := us.Unread(p.bus, time.Now()); len(items) != 0 {
		t.Fatalf("jump did not resolve the row: %+v", items)
	}
}

func TestRailRendersUnreadMarker(t *testing.T) {
	p, _ := paneWithStore(t)
	if _, err := p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "@you one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "@you two"}); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{"#general": 2}
	p.unreadFor = func(ch string) int { return counts[ch] }
	rail := p.rail()
	// #general is the active channel and renders with its marker.
	if !contains(rail, "●2") {
		t.Fatalf("rail missing ●2: %q", rail)
	}
	// A single unread renders the plain dot.
	counts["#general"] = 1
	if !contains(p.rail(), "●") || contains(p.rail(), "●1") {
		t.Fatalf("single unread should render bare dot: %q", p.rail())
	}
	// Zero unreads render nothing.
	counts["#general"] = 0
	if contains(p.rail(), "●") {
		t.Fatalf("no unread should render no dot: %q", p.rail())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
