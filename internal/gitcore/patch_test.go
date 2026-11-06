package gitcore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const hd = "diff --git a/f b/f\n--- a/f\n+++ b/f\n"

func TestPartialPatch(t *testing.T) {
	mixed := "@@ -1,5 +1,5 @@ func x\n a\n-b\n-c\n+B\n+C\n d\n e\n"
	// body indexes: 0 a, 1 -b, 2 -c, 3 +B, 4 +C, 5 d, 6 e
	tests := []struct {
		name    string
		hunk    string
		sel     []int
		reverse bool
		want    string
	}{
		{"stage one pair: the rest of - stays, the other + drops", mixed, []int{1, 3},
			false, "@@ -1,5 +1,5 @@ func x\n a\n-b\n c\n+B\n d\n e\n"},
		{"stage only additions: unselected - becomes context", mixed, []int{3, 4},
			false, "@@ -1,5 +1,7 @@ func x\n a\n b\n c\n+B\n+C\n d\n e\n"},
		{"stage only deletions: unselected + dropped", mixed, []int{1, 2},
			false, "@@ -1,5 +1,3 @@ func x\n a\n-b\n-c\n d\n e\n"},
		{"unstage one: unselected + stays, unselected - drops", mixed, []int{2, 4},
			true, "@@ -1,5 +1,5 @@ func x\n a\n-c\n B\n+C\n d\n e\n"},
		{"hunk at file start", "@@ -1,2 +1,3 @@\n+top\n a\n b\n", []int{0}, false,
			"@@ -1,2 +1,3 @@\n+top\n a\n b\n"},
		{"file start, nothing old left in the selection", "@@ -0,0 +1,2 @@\n+a\n+b\n", []int{1}, false,
			"@@ -0,0 +1 @@\n+b\n"},
		{"deleting the whole file", "@@ -1,2 +0,0 @@\n-a\n-b\n", []int{0, 1}, false,
			"@@ -1,2 +0,0 @@\n-a\n-b\n"},
		{"only deletions of part of a file", "@@ -1,2 +0,0 @@\n-a\n-b\n", []int{0}, false,
			"@@ -1,2 +1 @@\n-a\n b\n"},
		{"no newline: marker follows a kept line", "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
			[]int{1, 3}, false,
			"@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n"},
		{"no newline: marker dropped with its dropped line", "@@ -1 +1,3 @@\n a\n+b\n+c\n\\ No newline at end of file\n",
			[]int{1}, false,
			"@@ -1 +1,2 @@\n a\n+b\n"},
		{"no newline: marker kept on a line turned into context", "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+B\n",
			[]int{3}, false,
			"@@ -1,2 +1,3 @@\n a\n b\n\\ No newline at end of file\n+B\n"},
		{"trimmed blank context line", "@@ -1,3 +1,3 @@\n a\n\n-b\n+B\n", []int{2, 3}, false,
			"@@ -1,3 +1,3 @@\n a\n \n-b\n+B\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sel := map[int]bool{}
			for _, i := range tc.sel {
				sel[i] = true
			}
			got, err := PartialPatch(hd, tc.hunk, sel, tc.reverse)
			if err != nil {
				t.Fatal(err)
			}
			if got != hd+tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, hd+tc.want)
			}
		})
	}
}

func TestPartialPatchRefuses(t *testing.T) {
	h := "@@ -1,2 +1,2 @@\n a\n-b\n+B\n"
	if _, err := PartialPatch(hd, h, map[int]bool{0: true}, false); !errors.Is(err, ErrNoSelectedChange) {
		t.Fatalf("a context-only selection: %v", err)
	}
	if _, err := PartialPatch(hd, h, nil, false); !errors.Is(err, ErrNoSelectedChange) {
		t.Fatalf("empty selection: %v", err)
	}
	if _, err := PartialPatch(hd, "not a hunk\n", map[int]bool{0: true}, false); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, err := PartialPatch(hd, "@@ nonsense @@\n+a\n", map[int]bool{0: true}, false); err == nil {
		t.Fatal("bad header accepted")
	}
}

// F-151: the partial patches really apply with the user's git, staging
// and unstaging single lines of a hunk (including no newline at EOF).
func TestApplyPartialRealGit(t *testing.T) {
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	r := NewRunner(bin, BackgroundEnv(env))
	ctx := context.Background()
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, _, err := r.Run(ctx, repo, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(repo, "f"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("one\ntwo\nthree\nfour\nfive")
	git("add", "f")
	git("-c", "user.name=t", "-c", "user.email=t@x", "commit", "-qm", "base")

	// edit adjacent lines (one hunk) and the last line (no newline at EOF)
	write("ONE\nTWO\nthree\nfour\nFIVE")
	hunk := func(cached bool) (string, string) {
		args := []string{"diff", "--no-color", "--cached"}
		if !cached {
			args = []string{"diff", "--no-color"}
		}
		patch := git(append(args, "--", "f")...)
		i := strings.Index(patch, "@@")
		return patch[:i], patch[i:]
	}
	head, h := hunk(false)
	// body: 0 -one, 1 -two, 2 +ONE, 3 +TWO: stage just the second pair
	p, err := PartialPatch(head, h, map[int]bool{1: true, 3: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyPartial(ctx, repo, p, false); err != nil {
		t.Fatalf("stage: %v\n%s", err, p)
	}
	if got := git("show", ":f"); got != "one\nTWO\nthree\nfour\nfive" {
		t.Fatalf("index after staging line 2 = %q", got)
	}

	// the no-newline last line, selected alone
	head, h = hunk(false)
	lines := strings.Split(strings.TrimSuffix(h, "\n"), "\n")
	sel := map[int]bool{}
	for i, l := range lines[1:] {
		if l == "-five" || l == "+FIVE" {
			sel[i] = true
		}
	}
	p, err = PartialPatch(head, h, sel, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyPartial(ctx, repo, p, false); err != nil {
		t.Fatalf("stage last: %v\n%s", err, p)
	}
	if got := git("show", ":f"); got != "one\nTWO\nthree\nfour\nFIVE" {
		t.Fatalf("index after staging the last line = %q", got)
	}

	// unstage only line 2 again
	head, h = hunk(true)
	lines = strings.Split(strings.TrimSuffix(h, "\n"), "\n")
	sel = map[int]bool{}
	for i, l := range lines[1:] {
		if l == "-two" || l == "+TWO" {
			sel[i] = true
		}
	}
	p, err = PartialPatch(head, h, sel, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyPartial(ctx, repo, p, true); err != nil {
		t.Fatalf("unstage: %v\n%s", err, p)
	}
	if got := git("show", ":f"); got != "one\ntwo\nthree\nfour\nFIVE" {
		t.Fatalf("index after unstaging line 2 = %q", got)
	}
	// the work tree is never touched
	if b, _ := os.ReadFile(filepath.Join(repo, "f")); string(b) != "ONE\nTWO\nthree\nfour\nFIVE" {
		t.Fatalf("work tree changed: %q", b)
	}
}
