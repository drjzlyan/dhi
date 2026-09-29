package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/channelmeta"
	"github.com/drjzlyan/dhi/internal/ansi"
)

func TestChannelDepthKeys(t *testing.T) {
	m, ws, b := newSurfaceWithBus(t)
	cm, err := channelmeta.Open(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	m.pane.meta = cm
	p := m.pane
	m.sec = secChannels

	msg, err := b.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	p.cursor = 0

	// React via the picker.
	p.handleKey("+")
	if !p.reactPick {
		t.Fatal("+ did not open the reaction picker")
	}
	p.handleKey("1")
	if got := cm.Reactions("#general", msg.ID); len(got) != 1 || got[0] != "+1" {
		t.Fatalf("reactions = %v", got)
	}
	// Pin.
	p.handleKey("p")
	if !cm.IsPinned("#general", msg.ID) {
		t.Fatal("p did not pin")
	}
	// Edit the human message.
	p.handleKey("e")
	if !p.focus || p.editID != msg.ID {
		t.Fatalf("e: focus=%v editID=%d", p.focus, p.editID)
	}
	p.input = []rune("hello edited")
	p.handleKey("enter")
	if txt, _ := cm.EditedText("#general", msg.ID); txt != "hello edited" {
		t.Fatalf("edit = %q", txt)
	}
	if p.editID != 0 || p.focus {
		t.Fatal("edit did not clear on save")
	}

	// Rendering reflects edit, pin and reaction.
	rendered := ansi.Strip(strings.Join(p.transcriptRender([]bus.Message{msg}, 60, 10), "\n"))
	for _, want := range []string{"hello edited", "(edited)", "[+1]", "pin"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("transcript missing %q:\n%s", want, rendered)
		}
	}

	// Search filters.
	p.search = "edited"
	if n := len(p.visibleHistory()); n != 1 {
		t.Fatalf("search match = %d", n)
	}
	p.search = "absent"
	if n := len(p.visibleHistory()); n != 0 {
		t.Fatalf("search non-match = %d", n)
	}
	// `/` enters search; esc clears.
	p.search = ""
	p.handleKey("/")
	if !p.searching {
		t.Fatal("/ did not enter search")
	}
	for _, r := range "hello" {
		p.handleKey(string(r))
	}
	if p.search != "hello" {
		t.Fatalf("typed search = %q", p.search)
	}
	p.handleKey("enter")
	if p.searching || p.search != "hello" {
		t.Fatalf("enter: searching=%v search=%q", p.searching, p.search)
	}
	p.handleKey("/")
	p.handleKey("esc")
	if p.search != "" || p.searching {
		t.Fatalf("esc did not clear search: %q", p.search)
	}

	// An agent message cannot be edited.
	agentMsg, _ := b.Post(bus.Message{Channel: "#general", Author: "scout", Text: "agent says hi"})
	p.search = ""
	p.cursor = 0
	// find the agent message (second in history); select it via j.
	p.handleKey("j")
	p.handleKey("e")
	if p.editID == agentMsg.ID {
		t.Fatal("agent message entered edit mode")
	}
	if !strings.Contains(p.flash, "only your own") {
		t.Fatalf("refusal flash = %q", p.flash)
	}
}

func TestChannelDepthNilMetaDegrades(t *testing.T) {
	m, _, b := newSurfaceWithBus(t)
	p := m.pane
	p.meta = nil
	if _, err := b.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	p.cursor = 0
	p.handleKey("+")
	if !strings.Contains(p.flash, "unavailable") {
		t.Fatalf("nil meta react flash = %q", p.flash)
	}
	rendered := ansi.Strip(strings.Join(p.transcriptRender([]bus.Message{{Channel: "#general", Author: bus.Human, Text: "hi"}}, 40, 6), "\n"))
	if !strings.Contains(rendered, "hi") {
		t.Fatalf("nil-meta render = %q", rendered)
	}
}
