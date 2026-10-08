package dap

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubDlv writes a `dlv` script that announces addr and then idles.
func stubDlv(t *testing.T, line string) []string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '" + line + "'\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "dlv"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir + ":/usr/bin:/bin"}
}

func TestStartDelveConnectsToAnnouncedAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listener:", err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- c
		}
	}()
	env := stubDlv(t, "DAP server listening at: "+ln.Addr().String())
	c, err := StartDelve(context.Background(), t.TempDir(), env)
	if err != nil {
		t.Fatalf("StartDelve: %v", err)
	}
	select {
	case conn := <-accepted:
		conn.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("adapter was never dialed")
	}
	if err := c.Close(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Close: %v", err)
	}
}

func TestStartDelveNamedFailures(t *testing.T) {
	if _, err := StartDelve(context.Background(), t.TempDir(), []string{"PATH=/nonexistent"}); err == nil ||
		!strings.Contains(err.Error(), "delve adapter") {
		t.Fatalf("missing dlv err = %v", err)
	}

	old := adapterStartTimeout
	adapterStartTimeout = 200 * time.Millisecond
	defer func() { adapterStartTimeout = old }()
	env := stubDlv(t, "something unrelated")
	if _, err := StartDelve(context.Background(), t.TempDir(), env); err == nil ||
		!strings.Contains(err.Error(), "did not announce") {
		t.Fatalf("silent dlv err = %v", err)
	}

	// Announces an address nobody listens on.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	env = stubDlv(t, fmt.Sprintf("DAP server listening at: %s", addr))
	adapterStartTimeout = 20 * time.Second // generous: full-suite -race load slows script startup
	if _, err := StartDelve(context.Background(), t.TempDir(), env); err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("refused connect err = %v", err)
	}
}
