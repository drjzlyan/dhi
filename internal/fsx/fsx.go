// Package fsx holds the file primitives DHI's stores share (F-078):
// atomic, durable whole-file writes and advisory cross-process locks. A
// store written by two DHI processes (the TUI and an eval, two windows on
// one workspace) must neither tear a file nor lose an update.
package fsx

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomic replaces path with data durably: a uniquely named temp
// file in the same directory is written and fsynced, renamed over path,
// and the directory is fsynced so the rename survives a crash. Readers
// see the old file or the new one, never a mix.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("fsx: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("fsx: temp for %s: %w", path, err)
	}
	tmp := f.Name()
	fail := func(err error) error {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsx: write %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Chmod(perm); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("fsx: write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("fsx: rename %s: %w", path, err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // best effort: some filesystems refuse a dir fsync
		_ = d.Close()
	}
	return nil
}

// Locked runs fn while holding an exclusive advisory lock on path+".lock"
// (created on demand). Every writer of a shared file takes it, so a
// read-modify-write in one process never interleaves with another's.
func Locked(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("fsx: mkdir: %w", err)
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("fsx: lock %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := lockFile(f); err != nil {
		return fmt.Errorf("fsx: lock %s: %w", path, err)
	}
	defer func() { _ = unlockFile(f) }()
	return fn()
}

// LockFile takes an exclusive advisory lock on an open file (released by
// UnlockFile or closing it): appenders lock the log file itself.
func LockFile(f *os.File) error { return lockFile(f) }

// UnlockFile releases LockFile.
func UnlockFile(f *os.File) error { return unlockFile(f) }
