package dap

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A project venv's python wins over PATH (.venv/bin/python before python3).
func TestStartDebugpyPrefersProjectVenv(t *testing.T) {
	root := t.TempDir()
	vbin := filepath.Join(root, ".venv", "bin")
	if err := os.MkdirAll(vbin, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"seq":1,"type":"response","request_seq":1,"success":true,"command":"initialize"}`
	script := "#!/bin/sh\ncase \"$1\" in\n-c) exit 0;;\n-m) printf 'Content-Length: " + strconv.Itoa(len(body)) +
		"\\r\\n\\r\\n%s' '" + body + "'; exec sleep 30;;\nesac\n"
	if err := os.WriteFile(filepath.Join(vbin, "python"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := StartDebugpy(context.Background(), root, []string{"PATH=" + t.TempDir()}) // nothing on PATH
	if err != nil {
		t.Fatalf("venv python not used: %v", err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Call(ctx, "initialize", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
}
