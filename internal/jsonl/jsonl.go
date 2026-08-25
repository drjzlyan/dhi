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
