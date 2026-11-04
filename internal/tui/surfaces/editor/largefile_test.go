package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLargeFileWarning(t *testing.T) {
	ws, root := setupWorkspace(t)
	defer os.RemoveAll(root)

	// Create a 5.1 MiB text file.
	largeFile := filepath.Join(root, "repos", "alpha", "large.txt")
	data := bytes.Repeat([]byte("a\n"), (largeFileThreshold/2)+1) // just over 5 MiB
	if err := os.WriteFile(largeFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("test", ws)
	n := &node{kind: nodeFile, path: largeFile, name: "large.txt"}
	m.open(n)

	// Should show confirmation dialog, not open the file.
	if m.pendingOpen == nil {
		t.Error("expected pendingOpen to be set")
	}
	if m.pendingOpen.size != int64(len(data)) {
		t.Errorf("pendingOpen.size = %d, want %d", m.pendingOpen.size, len(data))
	}
	if len(m.bufs) != 0 {
		t.Errorf("expected no open buffers yet, got %d", len(m.bufs))
	}
}

func TestLargeFileConfirm(t *testing.T) {
	ws, root := setupWorkspace(t)
	defer os.RemoveAll(root)

	largeFile := filepath.Join(root, "repos", "alpha", "large.txt")
	data := bytes.Repeat([]byte("b\n"), (largeFileThreshold/2)+1)
	if err := os.WriteFile(largeFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("test", ws)
	n := &node{kind: nodeFile, path: largeFile, name: "large.txt"}
	m.open(n)

	if m.pendingOpen == nil {
		t.Fatal("expected pendingOpen to be set")
	}

	// Confirm with 'y'.
	if !m.pendingOpenKey("y") {
		t.Error("pendingOpenKey('y') should return true")
	}

	if m.pendingOpen != nil {
		t.Error("expected pendingOpen to be cleared after confirm")
	}
	if len(m.bufs) != 1 {
		t.Errorf("expected 1 open buffer, got %d", len(m.bufs))
	}
}

func TestLargeFileCancel(t *testing.T) {
	ws, root := setupWorkspace(t)
	defer os.RemoveAll(root)

	largeFile := filepath.Join(root, "repos", "alpha", "large.txt")
	data := bytes.Repeat([]byte("c\n"), (largeFileThreshold/2)+1)
	if err := os.WriteFile(largeFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("test", ws)
	n := &node{kind: nodeFile, path: largeFile, name: "large.txt"}
	m.open(n)

	if m.pendingOpen == nil {
		t.Fatal("expected pendingOpen to be set")
	}

	// Cancel with 'n'.
	if !m.pendingOpenKey("n") {
		t.Error("pendingOpenKey('n') should return true")
	}

	if m.pendingOpen != nil {
		t.Error("expected pendingOpen to be cleared after cancel")
	}
	if len(m.bufs) != 0 {
		t.Errorf("expected 0 open buffers, got %d", len(m.bufs))
	}
	if m.openPath != "" {
		t.Errorf("a cancelled open must not become the open path, got %q", m.openPath)
	}
}

func TestSmallFileNoWarning(t *testing.T) {
	ws, root := setupWorkspace(t)
	defer os.RemoveAll(root)

	// Create a small file (under 5 MiB).
	smallFile := filepath.Join(root, "repos", "alpha", "small.txt")
	data := []byte("small file\n")
	if err := os.WriteFile(smallFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("test", ws)
	n := &node{kind: nodeFile, path: smallFile, name: "small.txt"}
	m.open(n)

	// Should open directly without confirmation.
	if m.pendingOpen != nil {
		t.Error("expected pendingOpen to be nil for small file")
	}
	if len(m.bufs) != 1 {
		t.Errorf("expected 1 open buffer, got %d", len(m.bufs))
	}
}

func TestLargeBinaryNoWarning(t *testing.T) {
	ws, root := setupWorkspace(t)
	defer os.RemoveAll(root)

	// Create a 5.1 MiB binary file (contains NUL bytes).
	binaryFile := filepath.Join(root, "repos", "alpha", "large.bin")
	data := make([]byte, largeFileThreshold+1)
	data[1000] = 0 // Add a NUL byte to mark it as binary
	if err := os.WriteFile(binaryFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("test", ws)
	n := &node{kind: nodeFile, path: binaryFile, name: "large.bin"}
	m.open(n)

	// Binary file should not show warning, should try to open and get an error.
	if m.pendingOpen != nil {
		t.Error("expected pendingOpen to be nil for binary file")
	}
	// textbuf.OpenFile will reject the binary file with an error, which gets stored in searchErr.
	if m.searchErr == "" {
		t.Error("expected searchErr to be set for binary file")
	}
}

func TestLargeFileReopenSkipsPrompt(t *testing.T) {
	ws, root := setupWorkspace(t)
	defer os.RemoveAll(root)

	largeFile := filepath.Join(root, "repos", "alpha", "large.txt")
	if err := os.WriteFile(largeFile, bytes.Repeat([]byte("d\n"), largeFileThreshold/2+1), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New("test", ws)
	n := &node{kind: nodeFile, path: largeFile, name: "large.txt"}
	m.open(n)
	m.pendingOpenKey("enter")
	m.open(n) // already a tab: switch to it, no second prompt
	if m.pendingOpen != nil || len(m.bufs) != 1 {
		t.Fatalf("reopen: pending=%v bufs=%d", m.pendingOpen != nil, len(m.bufs))
	}
}
