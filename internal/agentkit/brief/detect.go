package brief

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/verifyplan"
)

// DefaultProcess and DefaultReview are what a fresh brief proposes when
// the repo says nothing about how the team works.
const (
	DefaultProcess = "Work from task cards: one branch per card, small focused commits, " +
		"open a pull request, address review, merge when checks pass."
	DefaultReview = "Every change goes through a pull request the user reviews before it merges."
)

// Detect drafts a brief from the repo: the name, the README's first
// paragraph, the stack from manifest files, the test commands, and CI.
// root is the workspace root; dirs are its member directories. Pure
// file reads — nothing runs.
func Detect(root string, dirs []string) Brief {
	if len(dirs) == 0 {
		dirs = []string{root}
	}
	b := Brief{Name: filepath.Base(root), Process: DefaultProcess, Review: DefaultReview}
	var stack, tests []string
	seen := map[string]bool{}
	add := func(list *[]string, v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			*list = append(*list, v)
		}
	}
	for _, d := range append([]string{root}, dirs...) {
		if b.Summary == "" {
			b.Summary = readmeSummary(d)
		}
		for _, s := range stackOf(d) {
			add(&stack, s)
		}
		for _, t := range verifyplan.Detect(d) {
			cmd := t.String()
			if d != root {
				if rel, err := filepath.Rel(root, d); err == nil && rel != "." {
					cmd += " (in " + rel + ")"
				}
			}
			add(&tests, cmd)
		}
	}
	if len(dirs) == 1 {
		if n := packageName(dirs[0]); n != "" {
			b.Name = n
		}
	}
	b.Stack = strings.Join(stack, ", ")
	if len(tests) > 0 {
		b.Testing = "Run before saying done: " + strings.Join(tests, "; ")
	}
	if ci := ciOf(root, dirs); ci != "" {
		b.Process += " CI runs on " + ci + "; keep it green."
	}
	return b
}

var (
	badgeRe = regexp.MustCompile(`^\s*(\[!\[|!\[|<|\[!)`)
	mdLink  = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
)

// readmeSummary is the first prose paragraph of dir's README, capped.
func readmeSummary(dir string) string {
	for _, name := range []string{"README.md", "README", "readme.md", "README.rst", "README.txt"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var para []string
		inFence := false
		for _, l := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "=") || strings.HasPrefix(t, "-") && strings.Trim(t, "-") == "" ||
				badgeRe.MatchString(t) {
				if len(para) > 0 {
					break
				}
				continue
			}
			para = append(para, t)
		}
		s := mdLink.ReplaceAllString(strings.Join(para, " "), "$1")
		s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
		if r := []rune(s); len(r) > 400 {
			s = string(r[:397]) + "..."
		}
		return s
	}
	return ""
}

// frameworks maps a package.json dependency to the name shown in the stack.
var frameworks = []struct{ dep, name string }{
	{"next", "Next.js"}, {"react", "React"}, {"vue", "Vue"}, {"svelte", "Svelte"},
	{"@angular/core", "Angular"}, {"express", "Express"}, {"fastify", "Fastify"},
	{"@nestjs/core", "NestJS"}, {"typescript", "TypeScript"}, {"tailwindcss", "Tailwind"},
	{"prisma", "Prisma"}, {"vite", "Vite"},
}

var pyFrameworks = []struct{ dep, name string }{
	{"django", "Django"}, {"fastapi", "FastAPI"}, {"flask", "Flask"}, {"pytest", "pytest"},
}

// stackOf names the languages and frameworks in dir's top-level files.
func stackOf(dir string) []string {
	var out []string
	has := func(n string) bool { _, err := os.Stat(filepath.Join(dir, n)); return err == nil }
	if has("go.mod") {
		out = append(out, "Go")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		out = append(out, "Node.js")
		var pj struct {
			Deps    map[string]string `json:"dependencies"`
			DevDeps map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(data, &pj) == nil {
			for _, f := range frameworks {
				if _, ok := pj.Deps[f.dep]; ok {
					out = append(out, f.name)
				} else if _, ok := pj.DevDeps[f.dep]; ok {
					out = append(out, f.name)
				}
			}
		}
	}
	if has("pyproject.toml") || has("requirements.txt") || has("setup.py") {
		out = append(out, "Python")
		var reqs []byte
		for _, n := range []string{"pyproject.toml", "requirements.txt"} {
			if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
				reqs = append(reqs, b...)
			}
		}
		low := "\n" + strings.ToLower(string(reqs))
		for _, f := range pyFrameworks {
			if regexp.MustCompile(`(?m)["'\s^]` + regexp.QuoteMeta(f.dep) + `\b`).MatchString(low) {
				out = append(out, f.name)
			}
		}
	}
	if has("Cargo.toml") {
		out = append(out, "Rust")
	}
	if has("pom.xml") || has("build.gradle") || has("build.gradle.kts") {
		out = append(out, "Java/JVM")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.csproj")); len(m) > 0 || has("global.json") {
		out = append(out, ".NET")
	}
	if has("Gemfile") {
		out = append(out, "Ruby")
	}
	if has("Dockerfile") || has("compose.yaml") || has("docker-compose.yml") {
		out = append(out, "Docker")
	}
	return out
}

// packageName is the project name a manifest declares, if any.
func packageName(dir string) string {
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pj struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &pj) == nil && pj.Name != "" {
			return pj.Name
		}
	}
	for _, n := range []string{"pyproject.toml", "Cargo.toml"} {
		var doc struct {
			Project struct{ Name string } `toml:"project"`
			Package struct{ Name string } `toml:"package"`
		}
		if _, err := toml.DecodeFile(filepath.Join(dir, n), &doc); err == nil {
			if doc.Project.Name != "" {
				return doc.Project.Name
			}
			if doc.Package.Name != "" {
				return doc.Package.Name
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			if mod, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
				return filepath.Base(strings.TrimSpace(mod))
			}
		}
	}
	return ""
}

// ciOf names the CI system configured in root or a member.
func ciOf(root string, dirs []string) string {
	var found []string
	seen := map[string]bool{}
	for _, d := range append([]string{root}, dirs...) {
		for path, name := range map[string]string{
			".github/workflows": "GitHub Actions", ".gitlab-ci.yml": "GitLab CI",
			".circleci": "CircleCI", "Jenkinsfile": "Jenkins", "azure-pipelines.yml": "Azure Pipelines",
		} {
			if _, err := os.Stat(filepath.Join(d, path)); err == nil && !seen[name] {
				seen[name] = true
				found = append(found, name)
			}
		}
	}
	sort.Strings(found)
	return strings.Join(found, " and ")
}
