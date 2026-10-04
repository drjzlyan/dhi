package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestChannelsBulkMarkReadKey(t *testing.T) {
	m, ws, b := newSurfaceWithBus(t)
	if err := os.MkdirAll(filepath.Join(ws.Root, workspace.DHIDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Post(bus.Message{Channel: "#general", Author: "scout", Text: "@you hi"}); err != nil {
		t.Fatal(err)
	}
	m.refreshPaneRail()
	p := m.pane
	if p.channelName() == "" {
		t.Fatalf("no active channel: %+v", p.channels)
	}

	var got string
	p.markAllRead = func(ch string) error { got = ch; return nil }
	p.handleKey("R")
	if got != p.channelName() || !strings.Contains(p.flash, "marked") {
		t.Fatalf("R: got=%q flash=%q channel=%q", got, p.flash, p.channelName())
	}

	// A nil seam degrades with a named notice, never silently.
	p.markAllRead = nil
	p.flash = ""
	p.handleKey("R")
	if !strings.Contains(p.flash, "unavailable") {
		t.Fatalf("nil-seam flash = %q", p.flash)
	}
}
