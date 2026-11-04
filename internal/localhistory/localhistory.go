// Package localhistory keeps a snapshot of every file DHI saves (F-155), so
// an overwritten edit can be compared with and restored. Snapshots live
// under the workspace's .dhi/history folder, one folder per file and one
// numbered file per snapshot. It imports nothing from the TUI.
package localhistory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MaxPerFile is how many snapshots one file keeps; the oldest go first.
	MaxPerFile = 50
	// MaxTotalBytes caps all snapshots together; the oldest across files go first.
	MaxTotalBytes = 64 << 20
	// MaxSnapshotBytes is the largest file that is snapshotted at all.
	MaxSnapshotBytes = 4 << 20
	// pruneEvery is how many snapshots pass between whole-store size checks.
	pruneEvery = 20
	ext        = ".snap"
)

// Entry is one stored snapshot.
type Entry struct {
	ID   string // file name inside the file's folder
	At   time.Time
	Size int
}

// Store writes and reads snapshots under one folder.
type Store struct {
	dir string
	mu  sync.Mutex
	wg  sync.WaitGroup
	n   int
}

// New returns a store rooted at dir (created on the first snapshot).
func New(dir string) *Store { return &Store{dir: dir} }

func (s *Store) fileDir(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8]))
}

// Snapshot stores text as the saved content of path, off the caller's
// goroutine so a save never waits on it. Text identical to the newest
// snapshot is dropped. Failures are silent: history is a convenience and
// must never fail a save.
func (s *Store) Snapshot(path, text string, at time.Time) {
	if len(text) > MaxSnapshotBytes {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.write(path, text, at)
	}()
}

// Wait blocks until pending snapshots are on disk.
func (s *Store) Wait() { s.wg.Wait() }

func (s *Store) write(path, text string, at time.Time) {
	dir := s.fileDir(path)
	if es := s.list(dir); len(es) > 0 {
		if last, err := os.ReadFile(filepath.Join(dir, es[0].ID)); err == nil && bytes.Equal(last, []byte(text)) {
			return
		}
	}
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	ns := at.UnixNano()
	var name string
	for ; ; ns++ { // two saves in one tick still get distinct names
		name = fmt.Sprintf("%019d%s", ns, ext)
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			break
		}
	}
	if os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644) != nil {
		return
	}
	for es := s.list(dir); len(es) > MaxPerFile; es = es[:len(es)-1] {
		_ = os.Remove(filepath.Join(dir, es[len(es)-1].ID))
	}
	if s.n%pruneEvery == 0 {
		s.pruneTotal()
	}
	s.n++
}

// list returns dir's snapshots, newest first.
func (s *Store) list(dir string) []Entry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, e := range ents {
		n, ok := strings.CutSuffix(e.Name(), ext)
		if !ok {
			continue
		}
		ns, err := strconv.ParseInt(n, 10, 64)
		info, ierr := e.Info()
		if err != nil || ierr != nil {
			continue
		}
		out = append(out, Entry{ID: e.Name(), At: time.Unix(0, ns), Size: int(info.Size())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// pruneTotal deletes the oldest snapshots across all files until the store
// fits MaxTotalBytes.
func (s *Store) pruneTotal() {
	type item struct {
		path, id string
		size     int
	}
	var all []item
	total := 0
	dirs, _ := os.ReadDir(s.dir)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(s.dir, d.Name())
		for _, e := range s.list(dir) {
			all = append(all, item{filepath.Join(dir, e.ID), e.ID, e.Size})
			total += e.Size
		}
	}
	if total <= MaxTotalBytes {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
	for _, it := range all {
		if total <= MaxTotalBytes {
			break
		}
		if os.Remove(it.path) == nil {
			total -= it.size
		}
	}
}

// List returns the snapshots of path, newest first.
func (s *Store) List(path string) []Entry {
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(s.fileDir(path))
}

// Read returns the content of one snapshot of path.
func (s *Store) Read(path, id string) (string, error) {
	if filepath.Base(id) != id || !strings.HasSuffix(id, ext) {
		return "", fmt.Errorf("bad snapshot id %q", id)
	}
	b, err := os.ReadFile(filepath.Join(s.fileDir(path), id))
	return string(b), err
}
