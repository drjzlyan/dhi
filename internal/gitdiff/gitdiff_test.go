package gitdiff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const basicPatch = `diff --git a/main.go b/main.go
index 3e0f8b2..a1b2c3d 100644
--- a/main.go
+++ b/main.go
@@ -1,5 +1,6 @@ package main
 func main() {
-	fmt.Println("hi")
+	fmt.Println("hello")
+	fmt.Println("world")
 }
 
`

func TestParseBasic(t *testing.T) {
	files := Parse(basicPatch)
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	if f.OldPath != "main.go" || f.NewPath != "main.go" {
		t.Errorf("paths = %q/%q", f.OldPath, f.NewPath)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 5 || h.NewStart != 1 || h.NewLines != 6 {
		t.Errorf("hunk header = %d,%d %d,%d", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	}
	if h.Header != "package main" {
		t.Errorf("section header = %q", h.Header)
	}
	want := []Line{
		{Kind: Ctx, OldNo: 1, NewNo: 1, Text: "func main() {"},
		{Kind: Del, OldNo: 2, Text: "\tfmt.Println(\"hi\")"},
		{Kind: Add, NewNo: 2, Text: "\tfmt.Println(\"hello\")"},
		{Kind: Add, NewNo: 3, Text: "\tfmt.Println(\"world\")"},
		{Kind: Ctx, OldNo: 3, NewNo: 4, Text: "}"},
		{Kind: Ctx, OldNo: 4, NewNo: 5, Text: ""},
	}
	for i := range want {
		if i >= len(h.Lines) {
			t.Fatalf("hunk has %d lines, want %d", len(h.Lines), len(want))
		}
		if h.Lines[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, h.Lines[i], want[i])
		}
	}
	adds, dels := f.Stat()
	if adds != 2 || dels != 1 {
		t.Errorf("stat = %d/%d, want 2/1", adds, dels)
	}
}

func TestParseMultiHunkRestart(t *testing.T) {
	patch := `diff --git a/big.txt b/big.txt
index 111..222 100644
--- a/big.txt
+++ b/big.txt
@@ -10,4 +10,3 @@ alpha
 one
-two
 three
 four
@@ -50,3 +49,4 @@ omega
 five
+five-and-half
 six
 seven
`
	f := Parse(patch)[0]
	if len(f.Hunks) != 2 {
		t.Fatalf("got %d hunks, want 2", len(f.Hunks))
	}
	h2 := f.Hunks[1]
	if h2.Lines[1].Kind != Add || h2.Lines[1].NewNo != 50 || h2.Lines[1].OldNo != 0 {
		t.Errorf("second hunk numbering wrong: %+v", h2.Lines[1])
	}
}

func TestParseNewAndDeletedFiles(t *testing.T) {
	patch := `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..789
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+alpha
+beta
diff --git a/old.txt b/old.txt
deleted file mode 100644
index 789..0000000
--- a/old.txt
+++ /dev/null
@@ -1,1 +0,0 @@
-alpha
`
	files := Parse(patch)
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	n, d := files[0], files[1]
	if !n.IsNew || n.NewMode != "100644" || n.OldPath != "" || n.Hunks[0].Lines[0].NewNo != 1 {
		t.Errorf("new-file parse wrong: %+v", n)
	}
	if !d.IsDeleted || d.OldMode != "100644" || d.NewPath != "" || d.Hunks[0].Lines[0].OldNo != 1 {
		t.Errorf("deleted-file parse wrong: %+v", d)
	}
}

func TestParseRenameAndModeOnly(t *testing.T) {
	patch := `diff --git a/before.go b/after.go
similarity index 100%
rename from before.go
rename to after.go
diff --git a/run.sh b/run.sh
old mode 100644
new mode 100755
`
	files := Parse(patch)
	r, m := files[0], files[1]
	if !r.IsRename || r.OldPath != "before.go" || r.NewPath != "after.go" || len(r.Hunks) != 0 {
		t.Errorf("rename parse wrong: %+v", r)
	}
	if m.OldMode != "100644" || m.NewMode != "100755" || m.IsBinary {
		t.Errorf("mode-only parse wrong: %+v", m)
	}
}

func TestParseBinary(t *testing.T) {
	patch := `diff --git a/logo.png b/logo.png
index 123..456 100644
Binary files a/logo.png and b/logo.png differ
diff --git a/blob.bin b/blob.bin
index 123..456 100644
GIT binary patch
literal 10
`
	files := Parse(patch)
	for i, f := range files {
		if !f.IsBinary || len(f.Hunks) != 0 {
			t.Errorf("file %d should be binary with no hunks: %+v", i, f)
		}
	}
}

func TestParseToleratesNoiseAndNoNewlineMarker(t *testing.T) {
	patch := `From abc Mon Sep 17 00:00:00 2001
Subject: [PATCH] something

commit message body
diff --git a/x.txt b/x.txt
index 111..222 100744
--- a/x.txt
+++ b/x.txt
@@ -1,2 +1,2 @@
 end
-old tail
+new tail
\ No newline at end of file
garbage that ends the hunk
@@ -9,9 +9,9 @@ never parsed
`
	files := Parse(patch)
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	// The garbage line closes hunk 1; the trailing header still opens a
	// valid (body-less) hunk 2 — headers are boundaries, never content.
	if len(f.Hunks) != 2 || len(f.Hunks[0].Lines) == 0 || len(f.Hunks[1].Lines) != 0 {
		t.Fatalf("want populated hunk + empty hunk, got %+v", f.Hunks)
	}
	last := f.Hunks[0].Lines[len(f.Hunks[0].Lines)-1]
	if last.Kind != Add || last.Text != "new tail" {
		t.Errorf("\\ No-newline marker leaked into content: %+v", last)
	}
}

func TestParseQuotedPathsAndEmptyInput(t *testing.T) {
	patch := "diff --git \"a/sp ace.txt\" \"b/sp ace.txt\"\nindex 111..222 100644\n--- \"a/sp ace.txt\"\n+++ \"b/sp ace.txt\"\n@@ -1 +1 @@\n-a\n+b\n"
	f := Parse(patch)[0]
	if f.DisplayPath() != "sp ace.txt" {
		t.Errorf("quoted path = %q", f.DisplayPath())
	}
	if got := Parse(""); len(got) != 0 {
		t.Errorf("empty patch returned %v", got)
	}
}

func TestParseCRLFAndTabTerminators(t *testing.T) {
	patch := "diff --git a/a.md b/a.md\r\nindex 111..222 100644\r\n--- a/a.md\t\r\n+++ b/a.md\t\r\n@@ -1 +1 @@\r\n-x\r\n+y\r\n"
	f := Parse(patch)[0]
	if f.Hunks[0].Lines[1].Text != "y" {
		t.Errorf("CRLF/tab handling wrong: %+v", f.Hunks[0].Lines)
	}
}

func TestPairReplaceBalanced(t *testing.T) {
	h := Hunk{Lines: []Line{
		{Kind: Ctx, OldNo: 1, NewNo: 1, Text: "ctx"},
		{Kind: Del, OldNo: 2, Text: "a"},
		{Kind: Add, NewNo: 2, Text: "b"},
	}}
	rows := Pair(h)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Left == nil || rows[0].Right == nil || rows[0].Left.Text != "ctx" || rows[0].Right.Text != "ctx" {
		t.Errorf("context row wrong: %+v", rows[0])
	}
	if rows[1].Left.Text != "a" || rows[1].Right.Text != "b" {
		t.Errorf("replace row wrong: %+v", rows[1])
	}
}

func TestPairPureInsertionAndDeletion(t *testing.T) {
	ins := Hunk{Lines: []Line{
		{Kind: Ctx, OldNo: 1, NewNo: 1},
		{Kind: Add, NewNo: 2, Text: "i1"},
		{Kind: Add, NewNo: 3, Text: "i2"},
	}}
	rows := Pair(ins)
	if len(rows) != 3 || rows[1].Left != nil || rows[1].Right.Text != "i1" || rows[2].Right.Text != "i2" {
		t.Errorf("insertion pairing wrong: %+v", rows)
	}

	del := Hunk{Lines: []Line{
		{Kind: Del, OldNo: 1, Text: "d1"},
		{Kind: Del, OldNo: 2, Text: "d2"},
	}}
	rows = Pair(del)
	if len(rows) != 2 || rows[0].Right != nil || rows[1].Left.Text != "d2" {
		t.Errorf("deletion pairing wrong: %+v", rows)
	}
}

func TestPairUnbalancedRunPadsShorterSide(t *testing.T) {
	h := Hunk{Lines: []Line{
		{Kind: Del, OldNo: 1, Text: "d1"},
		{Kind: Del, OldNo: 2, Text: "d2"},
		{Kind: Add, NewNo: 1, Text: "a1"},
	}}
	rows := Pair(h)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Left.Text != "d1" || rows[0].Right.Text != "a1" {
		t.Errorf("zipped row wrong: %+v", rows[0])
	}
	if rows[1].Left.Text != "d2" || rows[1].Right != nil {
		t.Errorf("padded row wrong: %+v", rows[1])
	}
}

// Fixture round-trip: the parsed model of testdata/multi.patch is stored
// as JSON next to it and compared byte-for-byte. Regenerate deliberately:
//
//	DHI_UPDATE_GOLDENS=1 go test ./internal/gitdiff/
func TestFixtureMultiFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "multi.patch"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(Parse(string(raw)), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "multi.golden.json")
	if os.Getenv("DHI_UPDATE_GOLDENS") != "" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("parsed model drifted from golden; regenerate with DHI_UPDATE_GOLDENS=1")
	}
}
