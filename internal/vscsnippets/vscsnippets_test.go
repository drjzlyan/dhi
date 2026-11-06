package vscsnippets

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `{
	// a comment, and a trailing comma below
	"Print": {
		"prefix": ["pr", "println"],
		"body": ["fmt.Println(${1:x})", "$0"], /* block */
		"description": "print it",
		"scope": "go, javascript",
	},
	"Any": {"prefix": "any//x", "body": "a"},
}`

func TestParse(t *testing.T) {
	got, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	by := map[string]Snippet{}
	for _, s := range got {
		by[s.Prefix] = s
	}
	p := by["println"]
	if p.Body != "fmt.Println(${1:x})\n$0" || p.Desc != "print it" || len(p.Scope) != 2 || p.Scope[1] != "javascript" {
		t.Fatalf("%+v", p)
	}
	if by["any//x"].Body != "a" { // // inside a string is not a comment
		t.Fatalf("%+v", by["any//x"])
	}
}

func TestMatchesScope(t *testing.T) {
	scoped := Snippet{Scope: []string{"go"}}
	if !scoped.Matches("go") || scoped.Matches("rust") || scoped.Matches("") {
		t.Fatal("scoped snippet matched the wrong languages")
	}
	if !(Snippet{}).Matches("rust") || !(Snippet{}).Matches("") {
		t.Fatal("unscoped snippet applies everywhere")
	}
}

func TestParseRejectsBadShapes(t *testing.T) {
	if _, err := Parse([]byte(`{"a":{"prefix":5,"body":"x"}}`)); err == nil {
		t.Fatal("a numeric prefix must be refused")
	}
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Fatal("garbage must be refused")
	}
}

func TestDirLoadsAndReportsBadFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".vscode")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.code-snippets"), []byte(sample), 0o644)
	os.WriteFile(filepath.Join(dir, "b.code-snippets"), []byte(`{oops`), 0o644)
	os.WriteFile(filepath.Join(dir, "c.json"), []byte(sample), 0o644) // not a .code-snippets file
	snips, sig, errs := Dir(root)
	if len(snips) != 3 || sig == "" || len(errs) != 1 {
		t.Fatalf("snips=%d sig=%q errs=%v", len(snips), sig, errs)
	}
	if s, _, _ := Dir(t.TempDir()); len(s) != 0 {
		t.Fatal("a repo without .vscode has no snippets")
	}
}
