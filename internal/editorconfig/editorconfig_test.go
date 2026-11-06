package editorconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		glob, rel string
		want      bool
	}{
		{"*", "a.go", true},
		{"*", "x/a.go", true}, // no slash: any depth
		{"*.go", "x/y/a.go", true},
		{"*.go", "a.py", false},
		{"/*.go", "a.go", true},
		{"/*.go", "x/a.go", false},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/x/a.go", false},
		{"src/**.go", "src/x/y/a.go", true},
		{"src/**/a.go", "src/a.go", true},
		{"src/**/a.go", "src/x/y/a.go", true},
		{"a?.go", "ab.go", true},
		{"a?.go", "a/.go", false},
		{"[ab].txt", "a.txt", true},
		{"[ab].txt", "c.txt", false},
		{"[!ab].txt", "c.txt", true},
		{"[!ab].txt", "a.txt", false},
		{"*.{js,ts}", "a.ts", true},
		{"*.{js,ts}", "a.go", false},
		{"*.{js,{ts,tsx}}", "a.tsx", true},
		{"file{1..3}.txt", "file2.txt", true},
		{"file{1..3}.txt", "file4.txt", false},
		{"file{-1..1}.txt", "file-1.txt", true},
		{"{one}.txt", "{one}.txt", true},
		{"{one}.txt", "one.txt", false},
		{"Makefile", "sub/Makefile", true},
		{`a\*.txt`, "a*.txt", true},
		{`a\*.txt`, "ab.txt", false},
		{"[unclosed", "[unclosed", true},
	}
	for _, c := range cases {
		if got := Match(c.glob, c.rel); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.glob, c.rel, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	f := Parse("# top\nroot = true\n\n[*]\nIndent_Style = Tab\n; note\n[*.md]\ntrim_trailing_whitespace=false\n")
	if !f.Root || len(f.Sections) != 2 {
		t.Fatalf("parse: %+v", f)
	}
	if f.Sections[0].Props["indent_style"] != "tab" || f.Sections[1].Props["trim_trailing_whitespace"] != "false" {
		t.Fatalf("props: %+v", f.Sections)
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLookupPrecedenceAndRoot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), "[*]\nindent_style = space\nindent_size = 2\nend_of_line = lf\n[*.go]\nindent_style = tab\n")
	write(t, filepath.Join(root, "sub", FileName), "[*.go]\nindent_size = 8\ninsert_final_newline = true\n[*.go]\ntrim_trailing_whitespace = true\n")
	r := NewResolver()

	c := r.Lookup(filepath.Join(root, "sub", "a.go"), root)
	if c.IndentStyle != "tab" || c.IndentSize != 8 || c.TabWidth != 8 || c.EndOfLine != "lf" {
		t.Fatalf("merged: %+v", c)
	}
	if c.FinalNL == nil || !*c.FinalNL || c.Trim == nil || !*c.Trim {
		t.Fatalf("bools: %+v", c)
	}
	c = r.Lookup(filepath.Join(root, "sub", "a.py"), root)
	if c.IndentStyle != "space" || c.IndentSize != 2 || c.FinalNL != nil {
		t.Fatalf("py: %+v", c)
	}

	// root = true in the nearer file stops the climb.
	write(t, filepath.Join(root, "sub", FileName), "root = true\n[*.go]\nindent_size = 3\n")
	r.Invalidate()
	c = r.Lookup(filepath.Join(root, "sub", "a.go"), root)
	if c.IndentStyle != "" || c.IndentSize != 3 || c.EndOfLine != "" {
		t.Fatalf("root stop: %+v", c)
	}
}

func TestLookupStopsAtMemberRoot(t *testing.T) {
	outer := t.TempDir()
	write(t, filepath.Join(outer, FileName), "[*]\nindent_style = tab\n")
	member := filepath.Join(outer, "m")
	write(t, filepath.Join(member, "a.txt"), "")
	r := NewResolver()
	if c := r.Lookup(filepath.Join(member, "a.txt"), member); c.IndentStyle != "" {
		t.Fatalf("climbed past the member: %+v", c)
	}
}

func TestResolveValues(t *testing.T) {
	c := resolve(map[string]string{"indent_size": "tab", "tab_width": "6", "indent_style": "unset", "insert_final_newline": "false"})
	if c.IndentSize != 6 || c.TabWidth != 6 || c.IndentStyle != "" || c.FinalNL == nil || *c.FinalNL {
		t.Fatalf("%+v", c)
	}
	c = resolve(map[string]string{"indent_size": "4"})
	if c.TabWidth != 4 {
		t.Fatalf("tab_width should default to indent_size: %+v", c)
	}
}

func TestInvalidateRereads(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), "[*]\nindent_size = 2\n")
	r := NewResolver()
	p := filepath.Join(root, "a.txt")
	if r.Lookup(p, root).IndentSize != 2 {
		t.Fatal("first")
	}
	write(t, filepath.Join(root, FileName), "[*]\nindent_size = 5\n")
	if r.Lookup(p, root).IndentSize != 2 {
		t.Fatal("expected cached value")
	}
	r.Invalidate()
	if r.Lookup(p, root).IndentSize != 5 {
		t.Fatal("expected fresh value")
	}
}
