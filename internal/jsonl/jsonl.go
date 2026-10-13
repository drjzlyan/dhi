// Package jsonl provides the two primitives every DHI log file needs:
// crash-safe line appends and full-file scans. Records are single-line
// JSON values separated by "\n" (ADR-0007 persistence format).
package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/drjzlyan/dhi/internal/fsx"
)

// Append writes one record as a final newline-terminated line, creating
// the parent directory when needed. Appends of < pipe-buffer size are
// atomic on local filesystems; readers tolerate trailing partial lines.
func Append(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("jsonl: mkdir: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("jsonl: marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("jsonl: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	// Locked so an append never lands inside a Repair's rewrite (F-078).
	if err := fsx.LockFile(f); err != nil {
		return fmt.Errorf("jsonl: lock %s: %w", path, err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("jsonl: append %s: %w", path, err)
	}
	return nil
}

// Read decodes every complete record into dst (a pointer to a slice
// element receiver — pass &item per call pattern: use ReadAll instead).
func Read(path string, into func(line []byte) error) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("jsonl: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := into(line); err != nil {
			return fmt.Errorf("jsonl: %s: %w", path, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("jsonl: scan %s: %w", path, err)
	}
	return nil
}

// ReadAll decodes every record into a slice of T. A torn final line
// (crash mid-append) is tolerated; corruption anywhere else errors.
func ReadAll[T any](path string) ([]T, error) {
	var lines [][]byte
	err := Read(path, func(line []byte) error {
		lines = append(lines, bytes.Clone(line))
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(lines))
	for i, line := range lines {
		var v T
		if err := json.Unmarshal(line, &v); err != nil {
			if i == len(lines)-1 {
				continue // torn tail from an interrupted append
			}
			return out[:0], fmt.Errorf("jsonl: %s: record %d: %w", path, i, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// ReadTolerant decodes every record it can (F-078): a corrupt line
// anywhere is skipped and counted instead of failing the whole read, so
// one bad byte never wipes a channel's history or an agent's journal.
// err is only for I/O. Callers repair with Repair.
func ReadTolerant[T any](path string) (out []T, skipped int, err error) {
	err = Read(path, func(line []byte) error {
		var v T
		if json.Unmarshal(line, &v) != nil {
			skipped++
			return nil
		}
		out = append(out, v)
		return nil
	})
	return out, skipped, err
}

// Tail returns up to n of the newest decodable records (oldest first),
// reading backwards from the end in chunks — a prompt needs the last few
// entries, not a scan of a file that grows forever. Corrupt lines are
// skipped. n <= 0 returns nil.
func Tail[T any](path string, n int) ([]T, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("jsonl: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("jsonl: stat %s: %w", path, err)
	}
	const chunk = 64 << 10
	var (
		rev  []T    // newest first
		rest []byte // partial line carried to the next (earlier) chunk
		pos  = st.Size()
	)
	for pos > 0 && len(rev) < n {
		size := int64(chunk)
		if pos < size {
			size = pos
		}
		pos -= size
		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, pos); err != nil {
			return nil, fmt.Errorf("jsonl: read %s: %w", path, err)
		}
		buf = append(buf, rest...)
		lines := bytes.Split(buf, []byte{'\n'})
		rest = lines[0] // may be cut mid-line unless pos == 0
		for i := len(lines) - 1; i >= 1 && len(rev) < n; i-- {
			if v, ok := decode[T](lines[i]); ok {
				rev = append(rev, v)
			}
		}
	}
	if pos == 0 && len(rev) < n {
		if v, ok := decode[T](rest); ok {
			rev = append(rev, v)
		}
	}
	out := make([]T, len(rev))
	for i, v := range rev {
		out[len(rev)-1-i] = v
	}
	return out, nil
}

func decode[T any](line []byte) (T, bool) {
	var v T
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return v, false
	}
	return v, json.Unmarshal(line, &v) == nil
}

// Repair rewrites path without its undecodable lines, appending each one
// to path+".corrupt" so nothing is silently lost (F-078). It holds the
// file's append lock and rewrites in place, so a concurrent Append waits
// rather than writing to a replaced file. A torn final line (a crash
// mid-append) is quarantined too. It reports the lines moved.
func Repair[T any](path string) (int, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("jsonl: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := fsx.LockFile(f); err != nil {
		return 0, fmt.Errorf("jsonl: lock %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("jsonl: read %s: %w", path, err)
	}
	var good, bad bytes.Buffer
	moved := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		t := bytes.TrimSpace(line)
		if len(t) == 0 {
			continue
		}
		if _, ok := decode[T](t); ok {
			good.Write(t)
			good.WriteByte('\n')
			continue
		}
		bad.Write(t)
		bad.WriteByte('\n')
		moved++
	}
	if moved == 0 {
		return 0, nil
	}
	// Keep a recovery copy until the in-place rewrite is done.
	backup := path + ".repair"
	if err := fsx.WriteAtomic(backup, data, 0o644); err != nil {
		return 0, err
	}
	q, err := os.OpenFile(path+".corrupt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("jsonl: quarantine %s: %w", path, err)
	}
	_, werr := q.Write(bad.Bytes())
	_ = q.Close()
	if werr != nil {
		return 0, fmt.Errorf("jsonl: quarantine %s: %w", path, werr)
	}
	if err := f.Truncate(0); err != nil {
		return 0, fmt.Errorf("jsonl: repair %s: %w", path, err)
	}
	if _, err := f.WriteAt(good.Bytes(), 0); err != nil {
		return 0, fmt.Errorf("jsonl: repair %s: %w (recovery copy: %s)", path, err, backup)
	}
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("jsonl: repair %s: %w", path, err)
	}
	_ = os.Remove(backup)
	return moved, nil
}
