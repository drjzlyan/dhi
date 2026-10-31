package textbuf

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrBinary refuses a file that holds a NUL byte in its first 8 KiB:
// editing it as lines would corrupt it on save (F-115).
var ErrBinary = errors.New("binary file — DHI edits text files only")

// ErrChangedOnDisk refuses a save over a file that something else (an
// agent's CLI, git, another editor) changed after the buffer read it
// (F-115). The user decides: reload or overwrite.
var ErrChangedOnDisk = errors.New("file changed on disk since it was read — :e! reloads it, :w! overwrites it")

// binarySniff is how much of a file is checked for NUL bytes.
const binarySniff = 8 << 10

// diskStamp is what the buffer last read from or wrote to its file.
type diskStamp struct {
	ok    bool // the file existed
	size  int64
	mtime time.Time
	sum   [sha256.Size]byte
}

func stampData(fi os.FileInfo, data []byte) diskStamp {
	return diskStamp{ok: true, size: fi.Size(), mtime: fi.ModTime(), sum: sha256.Sum256(data)}
}

// readStamp reads path's current stamp; a missing file is !ok, no error.
func readStamp(path string) (diskStamp, []byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return diskStamp{}, nil, nil
	}
	if err != nil {
		return diskStamp{}, nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return diskStamp{}, nil, err
	}
	return stampData(fi, data), data, nil
}

// isBinary reports a NUL byte in the first binarySniff bytes.
func isBinary(data []byte) bool {
	if len(data) > binarySniff {
		data = data[:binarySniff]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// encode renders the buffer as the bytes Save writes: lines joined by the
// file's own line ending. A trailing newline is the final empty line, so
// a file round-trips byte for byte.
func (b *Buffer) encode() []byte {
	nl := "\n"
	if b.crlf {
		nl = "\r\n"
	}
	var buf bytes.Buffer
	for i, l := range b.lines {
		if i > 0 {
			buf.WriteString(nl)
		}
		buf.WriteString(l)
	}
	return buf.Bytes()
}

// writeAtomic replaces path with data through a temp file and rename in
// the same folder, keeping the file's mode; a symlink is followed so the
// link itself survives.
func writeAtomic(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".dhi-*")
	if err != nil {
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	return nil
}

// ChangedOnDisk reports that the buffer's file no longer holds what the
// buffer last read or wrote. Cheap when nothing moved (size and mtime);
// it hashes only when they differ. A buffer never read from disk, or a
// file that cannot be read, reports false.
func (b *Buffer) ChangedOnDisk() bool {
	if b.diskPath == "" || !b.disk.ok {
		return false
	}
	fi, err := os.Stat(b.diskPath)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if fi.Size() == b.disk.size && fi.ModTime().Equal(b.disk.mtime) {
		return false
	}
	cur, _, err := readStamp(b.diskPath)
	return err == nil && cur.sum != b.disk.sum
}

// conflict is the save-time check: the file's content differs from the
// last read/write (hash, so a same-size rewrite is caught too).
func (b *Buffer) conflict(path string) (bool, error) {
	if b.diskPath != path || !b.disk.ok {
		return false, nil
	}
	cur, _, err := readStamp(path)
	if err != nil {
		return false, err
	}
	if !cur.ok { // deleted meanwhile: saving recreates it
		return false, nil
	}
	return cur.sum != b.disk.sum, nil
}

// NoteDisk re-reads the file's stamp after DHI itself wrote it outside
// the buffer (a formatter, a project replace), so DHI's own write is not
// mistaken for someone else's.
func (b *Buffer) NoteDisk() {
	if b.diskPath == "" {
		return
	}
	if st, _, err := readStamp(b.diskPath); err == nil {
		b.disk = st
	}
}

// NoteMoved follows the buffer's file to path after DHI moved it.
func (b *Buffer) NoteMoved(path string) {
	b.diskPath = path
	b.NoteDisk()
}

// DiskPath is the file the buffer was read from or last saved to.
func (b *Buffer) DiskPath() string { return b.diskPath }
