package fsx

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestWriteAtomicReplacesAndLeavesNoTemps(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.md")
	for _, body := range []string{"one", "two"} {
		if err := WriteAtomic(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Fatalf("content = %q", b)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
}

// Many goroutines doing read-modify-write under Locked never lose an
// increment (flock is per open file description, so this also covers
// two processes).
func TestLockedSerialisesReadModifyWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "counter")
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Locked(p, func() error {
				b, _ := os.ReadFile(p)
				n, _ := strconv.Atoi(string(b))
				return WriteAtomic(p, []byte(strconv.Itoa(n+1)), 0o644)
			})
		}()
	}
	wg.Wait()
	if b, _ := os.ReadFile(p); string(b) != "40" {
		t.Fatalf("counter = %s, want 40", b)
	}
}
