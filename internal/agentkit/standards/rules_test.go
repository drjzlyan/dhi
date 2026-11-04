package standards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func writeRule(t *testing.T, repo, name, text string) {
	t.Helper()
	p := filepath.Join(repo, ".claude", "rules", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRulesScopedIncludedAndExcluded(t *testing.T) {
	api := t.TempDir()
	writeRule(t, api, "lang/go.md", "---\npaths: [\"**/*.go\", \"go.mod\"]\n---\nWrap errors.\n")
	writeRule(t, api, "all.md", "Be brief.\n")
	rules := RepoRules([]workspace.Member{{Name: "api", Path: api}}, nil, "", false)
	if len(rules) != 2 {
		t.Fatalf("rules = %v", rules)
	}
	in := RulesBlock(rules, []string{"api/x.go"}, false)
	if !strings.Contains(in, "api/.claude/rules/lang/go.md (applies to **/*.go, go.mod):\nWrap errors.") || !strings.Contains(in, "Be brief.") {
		t.Fatalf("included:\n%s", in)
	}
	out := RulesBlock(rules, []string{"web/a.ts"}, false)
	if strings.Contains(out, "Wrap errors") || !strings.Contains(out, "Be brief.") {
		t.Fatalf("excluded:\n%s", out)
	}
	if prev := RulesBlock(rules, nil, true); !strings.Contains(prev, "applies to **/*.go, go.mod") {
		t.Fatalf("preview lists scoped rules with globs:\n%s", prev)
	}
	if RulesBlock(rules[1:], nil, false) != "" { // all.md sorts first, lang/go.md second
		t.Fatal("a scoped rule with no files: empty block")
	}
}

func TestRepoRulesSkipsTheCwdMemberWhenCLIReadsThem(t *testing.T) {
	api, web := t.TempDir(), t.TempDir()
	writeRule(t, api, "a.md", "api rule\n")
	writeRule(t, web, "w.md", "web rule\n")
	ms := []workspace.Member{{Name: "api", Path: api}, {Name: "web", Path: web}}
	got := RulesBlock(RepoRules(ms, nil, api, true), nil, false)
	if strings.Contains(got, "api rule") || !strings.Contains(got, "web rule") {
		t.Fatalf("claude: cwd member's rules are its own to load:\n%s", got)
	}
	got = RulesBlock(RepoRules(ms, nil, api, false), nil, false)
	if !strings.Contains(got, "api rule") || !strings.Contains(got, "web rule") {
		t.Fatalf("other CLIs get every member's rules:\n%s", got)
	}
	override := map[string]string{"web": api} // a task worktree stands in for web
	got = RulesBlock(RepoRules(ms, override, api, true), nil, false)
	if strings.Contains(got, "web rule") {
		t.Fatalf("roots override applies:\n%s", got)
	}
}

func TestRulesMalformedFrontmatterIsVisible(t *testing.T) {
	api := t.TempDir()
	writeRule(t, api, "bad.md", "---\npaths: [unclosed\n---\nx rule\n")
	got := RulesBlock(RepoRules([]workspace.Member{{Name: "api", Path: api}}, nil, "", false), nil, false)
	if !strings.Contains(got, "frontmatter ignored") || !strings.Contains(got, "x rule") {
		t.Fatalf("malformed rule must apply and say why:\n%s", got)
	}
}
