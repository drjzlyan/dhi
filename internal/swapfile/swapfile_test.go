package swapfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadRemove(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "swap"))
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	h := Header{Path: "/w/a.go", BaseHash: HashText("old"), BaseMTime: 7, At: at, PID: 42, Hot: true}
	s.Write(h, "new\ntext\n")
	s.Wait()
	got, text, ok := s.Read("/w/a.go")
	if !ok || text != "new\ntext\n" || got.PID != 42 || !got.Hot || got.BaseHash != HashText("old") || !got.At.Equal(at) {
		t.Fatalf("read = %+v %q %v", got, text, ok)
	}
	if _, _, ok := s.Read("/w/other.go"); ok {
		t.Fatal("another path must have no swap")
	}
	s.Remove("/w/a.go")
	if _, _, ok := s.Read("/w/a.go"); ok {
		t.Fatal("swap survived Remove")
	}
}

func TestRemoveWaitsForPendingWrite(t *testing.T) {
	s := New(t.TempDir())
	s.Write(Header{Path: "/w/a.go"}, "x")
	s.Remove("/w/a.go")
	if _, _, ok := s.Read("/w/a.go"); ok {
		t.Fatal("a pending write landed after Remove")
	}
}

func TestReadRejectsGarbage(t *testing.T) {
	s := New(t.TempDir())
	if err := s.WriteSync(Header{Path: "/w/a.go"}, "x"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.file("/w/a.go"), []byte("not json\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Read("/w/a.go"); ok {
		t.Fatal("garbage header accepted")
	}
}

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Fatal("own pid must be alive")
	}
	if PIDAlive(0) || PIDAlive(-3) {
		t.Fatal("non-positive pids are not alive")
	}
}
