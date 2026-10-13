package jsonl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type rec struct {
	N int `json:"n"`
}

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadTolerantSkipsCorruptLines(t *testing.T) {
	p := writeLines(t, `{"n":1}`, `{"n":2`, `garbage`, `{"n":3}`)
	got, skipped, err := ReadTolerant[rec](p)
	if err != nil || skipped != 2 || len(got) != 2 || got[1].N != 3 {
		t.Fatalf("got %v skipped %d err %v", got, skipped, err)
	}
	if _, err := ReadAll[rec](p); err == nil {
		t.Fatal("ReadAll stays strict")
	}
}

func TestTailReadsNewestAcrossChunks(t *testing.T) {
	var lines []string
	for i := 0; i < 5000; i++ {
		lines = append(lines, fmt.Sprintf(`{"n":%d,"pad":"%s"}`, i, strings.Repeat("x", 40)))
	}
	lines = append(lines, `{"n":5000`) // torn tail
	p := writeLines(t, lines...)
	got, err := Tail[rec](p, 3)
	if err != nil || len(got) != 3 || got[0].N != 4997 || got[2].N != 4999 {
		t.Fatalf("tail = %v %v", got, err)
	}
	all, _ := Tail[rec](p, 1<<20)
	if len(all) != 5000 || all[0].N != 0 {
		t.Fatalf("tail all = %d first %v", len(all), all[0])
	}
	if got, _ := Tail[rec](filepath.Join(t.TempDir(), "none"), 3); got != nil {
		t.Fatal("missing file must be empty")
	}
}

func TestRepairQuarantinesAndKeepsAppending(t *testing.T) {
	p := writeLines(t, `{"n":1}`, `not json`, `{"n":2}`)
	moved, err := Repair[rec](p)
	if err != nil || moved != 1 {
		t.Fatalf("moved %d err %v", moved, err)
	}
	if err := Append(p, rec{N: 3}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAll[rec](p)
	if err != nil || len(got) != 3 || got[2].N != 3 {
		t.Fatalf("after repair = %v %v", got, err)
	}
	q, _ := os.ReadFile(p + ".corrupt")
	if string(q) != "not json\n" {
		t.Fatalf("quarantine = %q", q)
	}
	if _, err := os.Stat(p + ".repair"); err == nil {
		t.Fatal("recovery copy left behind")
	}
	if moved, _ := Repair[rec](p); moved != 0 {
		t.Fatal("a clean file was rewritten")
	}
}
