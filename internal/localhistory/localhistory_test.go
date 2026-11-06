package localhistory

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestSnapshotListReadNewestFirst(t *testing.T) {
	s := New(t.TempDir())
	s.Snapshot("/w/a.go", "one", t0)
	s.Wait()
	s.Snapshot("/w/a.go", "two", t0.Add(time.Second))
	es := s.List("/w/a.go")
	if len(es) != 2 || es[0].Size != 3 || !es[0].At.After(es[1].At) {
		t.Fatalf("entries: %+v", es)
	}
	if got, _ := s.Read("/w/a.go", es[1].ID); got != "one" {
		t.Fatalf("oldest = %q", got)
	}
	if len(s.List("/w/b.go")) != 0 {
		t.Fatal("other file must have no history")
	}
}

func TestIdenticalConsecutiveSavesDedupe(t *testing.T) {
	s := New(t.TempDir())
	s.Snapshot("/w/a", "x", t0)
	s.Wait()
	s.Snapshot("/w/a", "x", t0.Add(time.Second))
	s.Wait()
	s.Snapshot("/w/a", "y", t0.Add(2*time.Second))
	s.Wait()
	s.Snapshot("/w/a", "x", t0.Add(3*time.Second)) // not consecutive: kept
	if n := len(s.List("/w/a")); n != 3 {
		t.Fatalf("want 3 entries, got %d", n)
	}
}

func TestSameInstantKeepsBoth(t *testing.T) {
	s := New(t.TempDir())
	s.Snapshot("/w/a", "x", t0)
	s.Wait()
	s.Snapshot("/w/a", "y", t0)
	if n := len(s.List("/w/a")); n != 2 {
		t.Fatalf("got %d", n)
	}
}

func TestPerFileCapPrunesOldest(t *testing.T) {
	s := New(t.TempDir())
	for i := 0; i < MaxPerFile+5; i++ {
		s.Snapshot("/w/a", fmt.Sprint("v", i), t0.Add(time.Duration(i)*time.Second))
		s.Wait()
	}
	es := s.List("/w/a")
	if len(es) != MaxPerFile {
		t.Fatalf("got %d entries", len(es))
	}
	if got, _ := s.Read("/w/a", es[len(es)-1].ID); got != "v5" {
		t.Fatalf("oldest kept = %q", got)
	}
}

func TestOversizeSkipped(t *testing.T) {
	s := New(t.TempDir())
	s.Snapshot("/w/a", string(make([]byte, MaxSnapshotBytes+1)), t0)
	if len(s.List("/w/a")) != 0 {
		t.Fatal("oversize file was snapshotted")
	}
}

func TestTotalCapPrunesOldestAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	big := string(make([]byte, MaxSnapshotBytes))
	for i := 0; i < 20; i++ { // 80 MiB, over the 64 MiB cap
		s.Snapshot(fmt.Sprint("/w/f", i), big, t0.Add(time.Duration(i)*time.Second))
		s.Wait()
	}
	s.mu.Lock()
	s.pruneTotal()
	s.mu.Unlock()
	total := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			fi, _ := d.Info()
			total += int(fi.Size())
		}
		return nil
	})
	if total > MaxTotalBytes {
		t.Fatalf("total %d over cap", total)
	}
	if len(s.List("/w/f0")) != 0 || len(s.List("/w/f19")) != 1 {
		t.Fatal("oldest must go first, newest stay")
	}
}

func TestReadRejectsTraversal(t *testing.T) {
	if _, err := New(t.TempDir()).Read("/w/a", "../x.snap"); err == nil {
		t.Fatal("traversal accepted")
	}
}
