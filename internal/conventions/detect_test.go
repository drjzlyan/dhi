package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferCommitsMajorities(t *testing.T) {
	conv := []string{"feat(ui): add x", "fix: y", "docs: z", "chore: bump", "feat: a", "refactor: b", "WIP"}
	if i := InferCommits(conv); i.Format != FormatConventional || i.MaxSubject != 72 || !strings.Contains(i.Evidence[0], "6 of 7") {
		t.Errorf("conventional: %+v", i)
	}
	tk := []string{"PROJ-1: a", "PROJ-2: b", "OPS-3 c", "PROJ-4: d", "PROJ-5: e", "Merge branch 'x'"}
	if i := InferCommits(tk); i.Format != FormatTicket || i.Subject != "{ticket}: {subject}" || i.TicketPattern == "" {
		t.Errorf("ticket: %+v", i)
	}
	mixed := []string{"a", "b", "fix: c", "PROJ-1: d", "e"}
	if i := InferCommits(mixed); i.Format != FormatFree {
		t.Errorf("mixed: %+v", i)
	}
	if i := InferCommits([]string{"fix: a", "fix: b"}); !i.Empty() {
		t.Errorf("too few: %+v", i)
	}
	var long []string
	for i := 0; i < 20; i++ {
		long = append(long, "fix: "+strings.Repeat("x", 90))
	}
	if i := InferCommits(long); i.MaxSubject != 100 {
		t.Errorf("long subjects: limit %d", i.MaxSubject)
	}
}

func TestInferFromExample(t *testing.T) {
	i := InferFromExample("PROJ-42: fix login\n\nWhy: tokens expired early.\nTested: unit\n\nCo-Authored-By: Bot <b@x>", "")
	if i.Format != FormatTicket || i.Subject != "{ticket}: {subject}" || i.Body != "Why: tokens expired early.\nTested: unit" {
		t.Fatalf("got %+v", i)
	}
	i = InferFromExample("", "## Summary\nAdds login.\n\n## Checklist\n- [x] tests\n- [ ] docs {later}\n")
	if i.PRBody != "## Summary\n\n## Checklist\n\n- [ ] tests\n- [ ] docs (later)" {
		t.Fatalf("pr skeleton %q", i.PRBody)
	}
	c := Defaults()
	c.Learn(i)
	if err := c.Validate(); err != nil {
		t.Fatalf("learned config invalid: %v", err)
	}
}

func TestPRTemplateAndGuidanceBody(t *testing.T) {
	dir := t.TempDir()
	if PRTemplate(dir) != "" {
		t.Fatal("no template expected")
	}
	os.MkdirAll(filepath.Join(dir, ".github"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github", "pull_request_template.md"), []byte("## What {x}\n"), 0o644)
	if got := PRTemplate(dir); got != "## What (x)" {
		t.Fatalf("template %q", got)
	}
	c := Defaults()
	c.Learn(Inference{Format: FormatConventional, Body: "Why:\nRisk:"})
	g := c.Guidance()
	if !strings.Contains(g, "Conventional Commits") || !strings.Contains(g, "    Why:\n    Risk:") {
		t.Fatalf("guidance:\n%s", g)
	}
	tpl := c.Snapshot()
	d := Defaults()
	d.Apply(tpl)
	if d.Commit.Body != "Why:\nRisk:" {
		t.Fatal("body lost through a template round trip")
	}
}
