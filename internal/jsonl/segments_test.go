package jsonl_test

import (
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/jsonl"
)

func TestSegmentsRotateAndTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.jsonl")
	for i := 1; i <= 6; i++ {
		if _, err := jsonl.Rotate(p, 30); err != nil {
			t.Fatal(err)
		}
		if err := jsonl.Append(p, rec{N: i}); err != nil {
			t.Fatal(err)
		}
	}
	if len(jsonl.SegmentPaths(p)) < 2 {
		t.Fatal("expected rotation")
	}
	all, err := jsonl.ReadAllSegments[rec](p)
	if err != nil || len(all) != 6 {
		t.Fatalf("all = %v %v", all, err)
	}
	for i, r := range all {
		if r.N != i+1 {
			t.Fatalf("order: %v", all)
		}
	}
	tail, _ := jsonl.TailSegments[rec](p, 4)
	if len(tail) != 4 || tail[0].N != 3 || tail[3].N != 6 {
		t.Fatalf("tail = %v", tail)
	}
}

func TestSegmentsAbsentAndNoOp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "none.jsonl")
	if ok, err := jsonl.Rotate(p, 10); ok || err != nil {
		t.Fatalf("rotate missing = %v %v", ok, err)
	}
	if all, err := jsonl.ReadAllSegments[rec](p); err != nil || len(all) != 0 {
		t.Fatalf("missing = %v %v", all, err)
	}
	_ = jsonl.Append(p, rec{N: 1})
	if ok, _ := jsonl.Rotate(p, 1<<20); ok {
		t.Fatal("rotated under threshold")
	}
}
