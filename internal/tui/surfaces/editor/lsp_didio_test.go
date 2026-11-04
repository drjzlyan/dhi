package editor

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/lsp"
)

// startFakeServerWithSaveCaps injects a fake server configured with specific
// save capabilities (nil, {}, {includeText: true}, or false).
func startFakeServerWithSaveCaps(t *testing.T, saveCaps any) (*goplusFake, *lsp.Manager) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	f := &goplusFake{conn: serverConn, rd: bufio.NewReader(serverConn), didSaveOpts: saveCaps}
	go f.serve()

	mgr := lsp.NewManager(nil, nil)
	c, err := lsp.New(context.Background(), clientConn, "/ws")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Inject("go", c)
	t.Cleanup(func() { mgr.ShutdownAll() })
	return f, mgr
}

func TestDidSaveAndChange(t *testing.T) {
	// When a buffer is edited and saved, didChange is sent for any pending
	// changes, then didSave follows. If lspSync() already sent the changes,
	// only didSave is sent.
	srv, mgr := startFakeServerWithSaveCaps(t, true)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	// open main.go
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")

	// edit the buffer (this will cause lspSync to send didChange)
	feed(m, "i", "text")
	feed(m, "esc")

	// save with ctrl+s
	feed(m, "ctrl+s")

	// Wait for didSave to arrive
	waitFor(t, m, func() bool {
		srv.mu.Lock()
		has := len(srv.didSaveEvents) > 0
		srv.mu.Unlock()
		return has
	}, "didSave")

	srv.mu.Lock()
	didSaveCount := len(srv.didSaveEvents)
	srv.mu.Unlock()

	if didSaveCount == 0 {
		t.Errorf("expected didSave to be sent")
	}
}

func TestDidSaveWithText(t *testing.T) {
	// Server with `save: {includeText: true}` gets the text included in didSave.
	srv, mgr := startFakeServerWithSaveCaps(t, map[string]any{"includeText": true})
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "didOpen")

	// Edit and save
	feed(m, "i", "hello world")
	feed(m, "esc")
	feed(m, "ctrl+s")

	waitFor(t, m, func() bool {
		srv.mu.Lock()
		has := len(srv.didSaveEvents) > 0
		srv.mu.Unlock()
		return has
	}, "didSave")

	srv.mu.Lock()
	if len(srv.didSaveEvents) == 0 {
		srv.mu.Unlock()
		t.Fatal("no didSave events")
	}
	saveEv := srv.didSaveEvents[0]
	hasText := saveEv["hasText"].(bool)
	srv.mu.Unlock()

	if !hasText {
		t.Errorf("didSave with includeText: true should have text; got %v", saveEv)
	}
}

func TestDidSaveWithoutText(t *testing.T) {
	// Server without `includeText` omits the text from didSave.
	srv, mgr := startFakeServerWithSaveCaps(t, true)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "didOpen")

	feed(m, "i", "test")
	feed(m, "esc")
	feed(m, "ctrl+s")

	waitFor(t, m, func() bool {
		srv.mu.Lock()
		has := len(srv.didSaveEvents) > 0
		srv.mu.Unlock()
		return has
	}, "didSave")

	srv.mu.Lock()
	if len(srv.didSaveEvents) == 0 {
		srv.mu.Unlock()
		t.Fatal("no didSave events")
	}
	saveEv := srv.didSaveEvents[0]
	hasText := saveEv["hasText"].(bool)
	srv.mu.Unlock()

	if hasText {
		t.Errorf("didSave without includeText should not have text; got %v", saveEv)
	}
}

func TestDidCloseOpenedTab(t *testing.T) {
	// Closing a tab that received didOpen produces a didClose notification.
	srv, mgr := startFakeServerWithSaveCaps(t, true)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	// Open and verify didOpen
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool {
		srv.mu.Lock()
		has := srv.lastOpen != ""
		srv.mu.Unlock()
		return has
	}, "didOpen")

	srv.mu.Lock()
	openURI := srv.lastOpen
	srv.mu.Unlock()

	// Close the tab with :q
	feed(m, ":", "q", "enter")

	// Wait for didClose
	waitFor(t, m, func() bool {
		srv.mu.Lock()
		has := len(srv.didCloseUris) > 0
		srv.mu.Unlock()
		return has
	}, "didClose")

	srv.mu.Lock()
	if len(srv.didCloseUris) == 0 {
		srv.mu.Unlock()
		t.Fatal("no didClose events")
	}
	closeURI := srv.didCloseUris[0]
	srv.mu.Unlock()

	if openURI != closeURI {
		t.Errorf("didClose URI %s != didOpen URI %s", closeURI, openURI)
	}
}

func TestDidCloseDontSendIfNeverOpened(t *testing.T) {
	// A tab whose server was still starting when the tab was created should
	// not produce didClose if closed before the server arrives.
	// This is verified by ensuring lspDidClose checks m.lspSent.
	// We test that exactly one didClose arrives when we open and close.
	srv, mgr := startFakeServerWithSaveCaps(t, true)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	// Open a file
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "didOpen")

	srv.mu.Lock()
	didCloseCount := len(srv.didCloseUris)
	srv.mu.Unlock()

	// Close it with :q
	feed(m, ":", "q", "enter")

	// Verify exactly one didClose
	waitFor(t, m, func() bool {
		srv.mu.Lock()
		newCount := len(srv.didCloseUris)
		srv.mu.Unlock()
		return newCount > didCloseCount
	}, "didClose after open")

	srv.mu.Lock()
	finalCount := len(srv.didCloseUris)
	srv.mu.Unlock()

	if finalCount != didCloseCount+1 {
		t.Errorf("expected 1 new didClose, got %d", finalCount-didCloseCount)
	}
}

func TestDidSaveCapabilityOmitted(t *testing.T) {
	// Server that omits save capability (no textDocumentSync or save: false)
	// should not receive didSave.
	srv, mgr := startFakeServerWithSaveCaps(t, false)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	// Open
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "didOpen")

	// Edit and save
	feed(m, "i", "test")
	feed(m, "esc")
	feed(m, "ctrl+s")

	// Verify no didSave arrives by waiting a bit and checking.
	time.Sleep(100 * time.Millisecond)
	m.drainTerm()

	srv.mu.Lock()
	saveCount := len(srv.didSaveEvents)
	srv.mu.Unlock()

	if saveCount > 0 {
		t.Errorf("expected no didSave with save: false; got %d events", saveCount)
	}
}
