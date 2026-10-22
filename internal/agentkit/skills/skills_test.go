package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"pdf-processing": true, "a": true, "code-review2": true,
		"PDF-Processing": false, "-pdf": false, "pdf-": false, "pdf--x": false, "": false,
		strings.Repeat("a", 65): false, "under_score": false,
	} {
		if ValidName(name) != want {
			t.Errorf("ValidName(%q) = %v", name, !want)
		}
	}
}

func TestParseLenientAsTheClientGuideSays(t *testing.T) {
	cases := []struct {
		name, dir, doc string
		ok             bool
		warn           string
	}{
		{"spec example", "pdf-processing", "---\nname: pdf-processing\ndescription: Extract PDF text. Use when handling PDFs.\nlicense: Apache-2.0\nmetadata:\n  author: example-org\n  version: \"1.0\"\n---\n# PDF\nSteps.\n", true, ""},
		{"unquoted colon is retried", "triage", "---\nname: triage\ndescription: Use this skill when: a pager fires\n---\nbody\n", true, ""},
		{"name differs: warn, load", "dir-name", "---\nname: other\ndescription: d\n---\nb", true, "differs"},
		{"missing name: dir name", "fallback", "---\ndescription: d\n---\nb", true, "missing"},
		{"vendor keys are fine", "x", "---\nname: x\ndescription: d\ndisable-model-invocation: true\nallowed-tools: Bash(git:*) Read\n---\nb", true, ""},
		{"no description: skip", "x", "---\nname: x\n---\nb", false, ""},
		{"no frontmatter: skip", "x", "# just markdown", false, ""},
		{"broken yaml: skip", "x", "---\nname: [x\ndescription: d\n---\nb", false, ""},
	}
	for _, c := range cases {
		s, err := Parse(c.dir, []byte(c.doc))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if err != nil {
			continue
		}
		if c.warn != "" && !strings.Contains(strings.Join(s.Warnings, ";"), c.warn) {
			t.Errorf("%s: warnings %v lack %q", c.name, s.Warnings, c.warn)
		}
		if c.warn == "" && len(s.Warnings) > 0 {
			t.Errorf("%s: unexpected warnings %v", c.name, s.Warnings)
		}
	}
	s, _ := Parse("pdf-processing", []byte(cases[0].doc))
	if s.License != "Apache-2.0" || s.Metadata["version"] != "1.0" || s.Body != "# PDF\nSteps." {
		t.Fatalf("fields: %+v", s)
	}
}

func TestWriteRoundTripAndScan(t *testing.T) {
	root := t.TempDir()
	in := &Skill{Name: "code-review", Description: "Review a diff. Use when asked to review.", Body: "1. Read.\n2. Comment.", Metadata: map[string]string{"author": "me"}}
	if err := Write(root, in); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, &Skill{Name: "Bad_Name", Description: "d", Body: "b"}); err == nil {
		t.Fatal("a non-spec name must not be written")
	}
	found := Scan(root)
	if len(found) != 1 || found[0].Err != nil {
		t.Fatalf("scan = %+v", found)
	}
	got := found[0].Skill
	if got.Description != in.Description || got.Body != in.Body || got.Metadata["author"] != "me" || got.Location() != filepath.Join(root, "code-review", File) {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestScriptStaysInsideTheSkill(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := Script(dir, "run.sh"); err != nil || p != filepath.Join(dir, "scripts", "run.sh") {
		t.Fatalf("short name: %q %v", p, err)
	}
	for _, bad := range []string{"../x", "/bin/sh", "scripts/../../x", ""} {
		if _, err := Script(dir, bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestCatalogAndActivation(t *testing.T) {
	if Catalog(nil) != "" {
		t.Fatal("no skills: no catalog")
	}
	dir := t.TempDir()
	s := &Skill{Name: "pdf", Description: "PDFs & forms", Body: "Do it."}
	if err := Write(dir, s); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(s.Dir, "references"), 0o755)
	_ = os.WriteFile(filepath.Join(s.Dir, "references", "REF.md"), []byte("x"), 0o644)
	cat := Catalog([]Skill{*s})
	for _, want := range []string{"<available_skills>", "<name>pdf</name>", "PDFs &amp; forms", "<location>" + s.Location() + "</location>"} {
		if !strings.Contains(cat, want) {
			t.Errorf("catalog lacks %q:\n%s", want, cat)
		}
	}
	act := Activation(*s)
	for _, want := range []string{`<skill_content name="pdf">`, "Do it.", "Skill directory: " + s.Dir, "<file>references/REF.md</file>"} {
		if !strings.Contains(act, want) {
			t.Errorf("activation lacks %q:\n%s", want, act)
		}
	}
}
