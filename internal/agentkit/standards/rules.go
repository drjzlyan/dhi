package standards

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/drjzlyan/dhi/internal/agentkit/pathscope"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Rules are the standard Claude Code project rules (F-152): Markdown
// files under .claude/rules/ (any depth) in a member repo, with optional
// YAML frontmatter. `paths:` is a list of globs (or one comma-separated
// string); a rule with paths enters the prompt only when the turn
// concerns a matching file, a rule without them always applies. Teams
// share them by committing the repo's .claude/rules, as for Claude Code.
const rulesSubdir = ".claude/rules"

// Rule is one rule file.
type Rule struct {
	Member string   // the repo it came from
	Name   string   // path under .claude/rules, without .md
	Paths  []string // empty = always applies
	Body   string
	Warn   string // frontmatter problem; the rule then applies to every turn
}

// RepoRules reads each member's .claude/rules, sorted by member then
// name. The claude CLI loads .claude/rules from its own working
// directory, so that member is left out when cliReads is true; every
// other member's rules are carried, because no CLI looks into sibling
// repos (the F-124 treatment of AGENTS.md). roots overrides a member's
// directory (a task worktree holds the branch's copy). A file whose
// frontmatter does not parse is kept, applies to every turn and carries
// Warn, so the problem is visible instead of silently dropping a rule.
func RepoRules(members []workspace.Member, roots map[string]string, cwd string, cliReads bool) []Rule {
	ms := append([]workspace.Member(nil), members...)
	sort.Slice(ms, func(i, j int) bool { return ms[i].Name < ms[j].Name })
	var out []Rule
	for _, m := range ms {
		dir := m.Path
		if r, ok := roots[m.Name]; ok && r != "" {
			dir = r
		}
		if cliReads && sameDir(dir, cwd) {
			continue
		}
		base := filepath.Join(dir, rulesSubdir)
		_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(base, p)
			r, perr := parseRule(strings.TrimSuffix(filepath.ToSlash(rel), ".md"), string(data))
			if perr != nil {
				r.Warn = perr.Error()
			}
			r.Member = m.Name
			if r.Body != "" {
				out = append(out, r)
			}
			return nil
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Member != out[j].Member {
			return out[i].Member < out[j].Member
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func parseRule(name, text string) (Rule, error) {
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\xef\xbb\xbf")
	r := Rule{Name: name, Body: strings.TrimSpace(text)}
	if !strings.HasPrefix(text, "---\n") {
		return r, nil
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return r, fmt.Errorf("frontmatter has no closing ---")
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return r, fmt.Errorf("frontmatter is not valid YAML: %v", err)
	}
	body := rest[end+len("\n---"):]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	r.Body = strings.TrimSpace(body)
	var raw []string
	switch t := fm["paths"].(type) {
	case nil:
	case []any:
		for _, x := range t {
			raw = append(raw, fmt.Sprint(x))
		}
	default:
		raw = strings.Split(fmt.Sprint(t), ",")
	}
	for _, g := range raw {
		if g = strings.TrimSpace(g); g != "" {
			r.Paths = append(r.Paths, g)
		}
	}
	return r, nil
}

// RulesBlock renders the rules that apply to files. Unscoped rules
// always ride; a scoped rule rides only when one of its globs matches a
// file, and is labelled with its globs. all lists every rule instead (the
// Settings preview, which has no turn): scoped rules then say when they
// apply. "" when nothing applies.
func RulesBlock(rules []Rule, files []string, all bool) string {
	var b strings.Builder
	for _, r := range rules {
		scoped := len(r.Paths) > 0
		if scoped && !all && !pathscope.Any(r.Paths, files) {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("Rules (follow them when they apply to the files you work on):")
		}
		b.WriteString("\n\n" + r.Member + "/.claude/rules/" + r.Name + ".md")
		if r.Warn != "" {
			b.WriteString(" (frontmatter ignored: " + r.Warn + "; applies to every turn)")
		} else if scoped {
			b.WriteString(" (applies to " + strings.Join(r.Paths, ", ") + ")")
		}
		b.WriteString(":\n" + r.Body)
	}
	return b.String()
}
