package gitcore

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const us = "\x1f"

// F-162: the graph parser keeps lane art, splits fields and decorations, and
// keeps connector lines as rows without a hash.
func TestParseGraph(t *testing.T) {
	out := strings.Join([]string{
		"*   " + us + "aaaa1111" + us + "aaaa111" + us + "bbbb2222 cccc3333" + us +
			"HEAD -> refs/heads/main, tag: refs/tags/v1.0, refs/remotes/origin/main, refs/heads/feat/x" + us + "2 hours ago" + us + "Ada" + us + "merge: a | b",
		"|\\  ",
		"| * " + us + "cccc3333" + us + "cccc333" + us + "dddd4444" + us + "" + us + "3 days ago" + us + "Bob" + us + "side",
		"|/  ",
		"* " + us + "bbbb2222" + us + "bbbb222" + us + "" + us + "HEAD, refs/stash" + us + "1 week ago" + us + "Ada" + us + "root",
	}, "\n") + "\n"
	rows := ParseGraph(out)
	if len(rows) != 5 {
		t.Fatalf("rows: %d %+v", len(rows), rows)
	}
	m := rows[0]
	if !m.IsCommit() || m.Short != "aaaa111" || m.Author != "Ada" || m.When != "2 hours ago" || m.Subject != "merge: a | b" {
		t.Fatalf("merge row: %+v", m)
	}
	if m.Graph != "*   " || len(m.Parents) != 2 || m.Parents[1] != "cccc3333" {
		t.Fatalf("graph/parents: %+v", m)
	}
	want := []GraphRef{
		{Name: "HEAD", Kind: RefHead},
		{Name: "main", Kind: RefBranch, Current: true},
		{Name: "v1.0", Kind: RefTag},
		{Name: "origin/main", Kind: RefRemote},
		{Name: "feat/x", Kind: RefBranch},
	}
	if len(m.Refs) != len(want) {
		t.Fatalf("refs: %+v", m.Refs)
	}
	for i, w := range want {
		if m.Refs[i] != w {
			t.Fatalf("ref %d: got %+v want %+v", i, m.Refs[i], w)
		}
	}
	if rows[1].IsCommit() || rows[1].Graph != "|\\" || rows[3].IsCommit() {
		t.Fatalf("connectors: %+v %+v", rows[1], rows[3])
	}
	if rows[2].Graph != "| * " || len(rows[2].Refs) != 0 {
		t.Fatalf("side row: %+v", rows[2])
	}
	last := rows[4]
	if len(last.Parents) != 0 || last.Refs[0].Kind != RefHead || last.Refs[1].Kind != RefOther {
		t.Fatalf("root row: %+v", last)
	}
	if ParseGraph("") != nil {
		t.Fatal("empty output has no rows")
	}
}

// F-162: a real two-branch repo lists every branch's commits, cherry-picks one
// cleanly, stops on a conflicting pick, and the abort restores the tree.
func TestGraphCherryPickAndBranch(t *testing.T) {
	r, dir := stashRepo(t)
	ctx := context.Background()
	run := func(args ...string) string {
		t.Helper()
		out, _, err := r.Run(ctx, dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(out)
	}
	write := func(name, body string) { _ = os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) }
	commit := func(msg string) string {
		run("add", "-A")
		run("commit", "-qm", msg)
		return run("rev-parse", "HEAD")
	}
	base := run("rev-parse", "--abbrev-ref", "HEAD")

	run("checkout", "-q", "-b", "feat")
	write("b.txt", "bee\n")
	clean := commit("add b")
	write("a.txt", "feature\n")
	clash := commit("feature a")

	run("checkout", "-q", base)
	write("a.txt", "mainline\n")
	commit("main a")

	rows, err := GraphLog(ctx, r, dir, 500)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, row := range rows {
		if row.IsCommit() {
			subjects = append(subjects, row.Subject)
		}
	}
	sort.Strings(subjects) // commits within one second order by topology, not time
	if strings.Join(subjects, ",") != "add b,feature a,main a,one" {
		t.Fatalf("subjects: %v", subjects)
	}
	var sawHead, sawFeat bool
	for _, row := range rows {
		for _, ref := range row.Refs {
			sawHead = sawHead || (ref.Kind == RefBranch && ref.Name == base && ref.Current)
			sawFeat = sawFeat || (ref.Kind == RefBranch && ref.Name == "feat")
		}
	}
	if !sawHead || !sawFeat {
		t.Fatalf("decorations missing: head=%v feat=%v", sawHead, sawFeat)
	}

	// Clean pick.
	if err := CherryPick(ctx, r, dir, clean); err != nil {
		t.Fatal(err)
	}
	if InCherryPick(ctx, r, dir) {
		t.Fatal("a clean pick must not leave a pick in progress")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal("b.txt should have arrived")
	}

	// Conflicting pick.
	if err := CherryPick(ctx, r, dir, clash); err == nil {
		t.Fatal("expected a conflict")
	}
	if !InCherryPick(ctx, r, dir) {
		t.Fatal("a conflicting pick stops in progress")
	}
	cs, err := Conflicts(ctx, r, dir)
	if err != nil || len(cs) != 1 || cs[0].Path != "a.txt" {
		t.Fatalf("conflicts: %+v %v", cs, err)
	}
	if err := CherryPickAbort(ctx, r, dir); err != nil {
		t.Fatal(err)
	}
	if InCherryPick(ctx, r, dir) {
		t.Fatal("abort ends the pick")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "mainline\n" {
		t.Fatalf("abort should restore a.txt, got %q", b)
	}

	// Branch at a commit, without switching.
	if err := BranchAt(ctx, r, dir, "topic", clash); err != nil {
		t.Fatal(err)
	}
	if got := run("rev-parse", "topic"); got != clash {
		t.Fatalf("topic at %s want %s", got, clash)
	}
	if got := run("rev-parse", "--abbrev-ref", "HEAD"); got != base {
		t.Fatalf("HEAD moved to %s", got)
	}
	if err := BranchAt(ctx, r, dir, "topic", clash); err == nil {
		t.Fatal("an existing branch name must be refused")
	}
	if err := BranchAt(ctx, r, dir, " ", clash); err == nil {
		t.Fatal("an empty name must be refused")
	}
}
