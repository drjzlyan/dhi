package jsonl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/jsonl"
)

type rec struct {
	N int    `json:"n"`
	S string `json:"s"`
}

func TestAppendReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "log.jsonl")
	for i := range 3 {
		if err := jsonl.Append(path, rec{N: i, S: "x"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	got, err := jsonl.ReadAll[rec](path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 3 || got[0].N != 0 || got[2].S != "x" {
		t.Errorf("got = %+v", got)
	}
}

func TestReadMissingIsEmpty(t *testing.T) {
	got, err := jsonl.ReadAll[rec](filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func TestToleratesPartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(path, []byte("{\"n\":1,\"s\":\"a\"}\n{\"n\":2,\"s\":\"b"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := jsonl.ReadAll[rec](path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].N != 1 {
		t.Errorf("got = %+v, want only the complete record", got)
	}
}
