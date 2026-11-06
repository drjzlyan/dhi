package jsonl

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/fsx"
)

// Segmented logs (F-153, ADR-0037). A log that grows forever is rotated by
// size: the active file is renamed to <name>.seg0001, <name>.seg0002, ...
// (oldest first) and a fresh active file starts. Readers go through the
// *Segments helpers, which read the segments in order and then the active
// file, so a log written before segmentation existed (one file, no
// segments) reads exactly as before.

const segMarker = ".seg"

// SegmentPaths lists path's rotated segments, oldest first.
func SegmentPaths(path string) []string {
	dir, base := filepath.Dir(path), filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type seg struct {
		n    int
		name string
	}
	var segs []seg
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), base+segMarker)
		if !ok || e.IsDir() {
			continue
		}
		n, err := strconv.Atoi(rest)
		if err != nil || n <= 0 {
			continue
		}
		segs = append(segs, seg{n, e.Name()})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].n < segs[j].n })
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = filepath.Join(dir, s.name)
	}
	return out
}

// Files is every file of the log in reading order: segments, then the
// active file (which may not exist).
func Files(path string) []string { return append(SegmentPaths(path), path) }

// Rotate moves the active file aside as the next segment once it holds
// maxBytes or more, reporting whether it did. It runs under the append
// lock, and Append notices the swap and reopens, so no record is lost.
func Rotate(path string, maxBytes int64) (bool, error) {
	if maxBytes <= 0 {
		return false, nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("jsonl: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := fsx.LockFile(f); err != nil {
		return false, fmt.Errorf("jsonl: lock %s: %w", path, err)
	}
	st, err := f.Stat()
	if err != nil || st.Size() < maxBytes {
		return false, err
	}
	next := 1
	if segs := SegmentPaths(path); len(segs) > 0 {
		last := segs[len(segs)-1]
		n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(last), filepath.Base(path)+segMarker))
		next = n + 1
	}
	dst := fmt.Sprintf("%s%s%04d", path, segMarker, next)
	if err := os.Rename(path, dst); err != nil {
		return false, fmt.Errorf("jsonl: rotate %s: %w", path, err)
	}
	return true, nil
}

// ReadAllSegments is ReadAll over the whole segmented log.
func ReadAllSegments[T any](path string) ([]T, error) {
	var out []T
	for _, p := range Files(path) {
		part, err := ReadAll[T](p)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

// ReadTolerantSegments is ReadTolerant over the whole segmented log.
func ReadTolerantSegments[T any](path string) (out []T, skipped int, err error) {
	for _, p := range Files(path) {
		part, sk, err := ReadTolerant[T](p)
		if err != nil {
			return nil, skipped, err
		}
		out = append(out, part...)
		skipped += sk
	}
	return out, skipped, nil
}

// TailSegments is Tail across segments: the newest n records, reaching
// into older segments only when the active file holds fewer.
func TailSegments[T any](path string, n int) ([]T, error) {
	files := Files(path)
	var out []T
	for i := len(files) - 1; i >= 0 && len(out) < n; i-- {
		part, err := Tail[T](files[i], n-len(out))
		if err != nil {
			return nil, err
		}
		out = append(part, out...)
	}
	return out, nil
}

// Stamp summarises the log's files (count, total size, newest mtime in
// nanoseconds) so a cache can tell when any segment or the active file
// changed.
func Stamp(path string) (files int, size, mod int64) {
	for _, p := range Files(path) {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		files++
		size += st.Size()
		if m := st.ModTime().UnixNano(); m > mod {
			mod = m
		}
	}
	return files, size, mod
}
