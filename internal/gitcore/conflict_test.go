package gitcore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseConflicts(t *testing.T) {
	out := "UU a.go\x00 M b.go\x00R  new.go\x00UU old.go\x00AA c.go\x00DU d go.txt\x00?? e\x00"
	want := []Conflicted{{"a.go", "UU"}, {"c.go", "AA"}, {"d go.txt", "DU"}}
	if got := parseConflicts(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := parseConflicts(""); len(got) != 0 {
		t.Fatalf("empty = %+v", got)
	}
}

// F-156: a real merge conflict is listed, and StagePath clears it.
func TestConflictsAndStagePath(t *testing.T) {
	r, dir := stashRepo(t)
	ctx := context.Background()
	file := filepath.Join(dir, "a.txt")
	run := func(args ...string) {
		t.Helper()
		if _, _, err := r.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("checkout", "-qb", "side")
	_ = os.WriteFile(file, []byte("side\n"), 0o644)
	run("commit", "-qam", "side")
	run("checkout", "-q", "-")
	_ = os.WriteFile(file, []byte("main\n"), 0o644)
	run("commit", "-qam", "main")
	if _, _, err := r.Run(ctx, dir, "merge", "side"); err == nil {
		t.Fatal("expected a conflicting merge")
	}
	got, err := Conflicts(ctx, r, dir)
	if err != nil || len(got) != 1 || got[0].Path != "a.txt" || got[0].Code != "UU" {
		t.Fatalf("conflicts = %+v, %v", got, err)
	}
	_ = os.WriteFile(file, []byte("resolved\n"), 0o644)
	if err := StagePath(ctx, r, dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Conflicts(ctx, r, dir); len(got) != 0 {
		t.Fatalf("still conflicted: %+v", got)
	}
}
