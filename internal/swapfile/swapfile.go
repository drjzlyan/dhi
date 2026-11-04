// Package swapfile keeps the unsaved text of an open buffer on disk (F-161),
// so a crash or a deliberate hot exit loses nothing. One file per buffer
// under the workspace's .dhi/tmp/swap: a one-line JSON header, a newline,
// then the buffer text. It imports nothing from the TUI.
package swapfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	version = 1
	ext     = ".swp"
)

// Header describes the buffer a swap belongs to.
type Header struct {
	Version   int       `json:"version"`
	Path      string    `json:"path"`
	BaseHash  string    `json:"base_hash"`  // HashText of the disk text the buffer was loaded from
	BaseMTime int64     `json:"base_mtime"` // that file's modification time, unix nanoseconds
	At        time.Time `json:"at"`
	PID       int       `json:"pid"`
	Hot       bool      `json:"hot,omitempty"` // left on purpose by "keep for next time"
}

// HashText is the digest recorded as a swap's base.
func HashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Store reads and writes swap files under one folder.
type Store struct {
	dir string
	mu  sync.Mutex
	wg  sync.WaitGroup
}

// New returns a store rooted at dir (created on the first write).
func New(dir string) *Store { return &Store{dir: dir} }

func (s *Store) file(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8])+ext)
}

// Write stores text off the caller's goroutine; errors are dropped, a swap
// must never get in the way of typing. Use WriteSync where failure matters.
func (s *Store) Write(h Header, text string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = s.WriteSync(h, text)
	}()
}

// WriteSync stores text and reports a failure.
func (s *Store) WriteSync(h Header, text string) error {
	h.Version = version
	head, err := json.Marshal(h)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	name := s.file(h.Path)
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, append(append(head, '\n'), text...), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}

// Read returns path's swap. ok is false when there is none or it cannot be
// parsed.
func (s *Store) Read(path string) (h Header, text string, ok bool) {
	data, err := os.ReadFile(s.file(path))
	if err != nil {
		return h, "", false
	}
	head, body, found := strings.Cut(string(data), "\n")
	if !found || json.Unmarshal([]byte(head), &h) != nil || h.Version != version || h.Path != path {
		return Header{}, "", false
	}
	return h, body, true
}

// Remove deletes path's swap after any pending write has landed.
func (s *Store) Remove(path string) {
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(s.file(path))
}

// Wait blocks until pending writes are on disk.
func (s *Store) Wait() { s.wg.Wait() }

// PIDAlive reports whether a process with this id is running.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
