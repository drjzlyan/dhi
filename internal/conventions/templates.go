package conventions

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// DefaultWorktree is the task worktree folder pattern before F-075:
// .dhi/tasks/<slug>/<member>.
const DefaultWorktree = "{slug}/{member}"

// Template is one named naming scheme (F-075): how task branches and
// worktree folders are named, what commit subjects look like, and the
// PR title and description. Empty fields keep the current rule.
type Template struct {
	Description string   `toml:"description,omitempty"`
	Branch      Branch   `toml:"branch"`
	Worktree    Worktree `toml:"worktree"`
	Commit      struct {
		Format        string `toml:"format,omitempty"`
		TicketPattern string `toml:"ticket_pattern,omitempty"`
		Subject       string `toml:"subject,omitempty"`
	} `toml:"commit"`
	PR PR `toml:"pr"`
}

// segmentBadRe is what a worktree folder segment may not hold: values
// never add separators or dots, so they cannot navigate.
var segmentBadRe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

var templateNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidTemplateName reports whether name can key a user template.
func ValidTemplateName(name string) bool { return templateNameRe.MatchString(name) }

// Builtin is a template DHI ships; user templates may copy, not edit, it.
type Builtin struct {
	Name string
	Template
}

// Builtins are the shipped schemes, in display order.
func Builtins() []Builtin {
	mk := func(name, desc, branch, wt, format, subject, title, body string) Builtin {
		t := Template{Description: desc, Branch: Branch{Task: branch, Review: "review/{id}"},
			Worktree: Worktree{Task: wt}, PR: PR{Title: title, Body: body}}
		t.Commit.Format, t.Commit.TicketPattern, t.Commit.Subject = format, `[A-Z][A-Z0-9]+-\d+`, subject
		return Builtin{Name: name, Template: t}
	}
	return []Builtin{
		mk("default", "task/<slug> branches, free-form commits", "task/{slug}", DefaultWorktree,
			FormatFree, "", "{title}", "Created from DHI worktree `{branch}`."),
		mk("feature", "feature/<slug> branches, Conventional Commits", "feature/{slug}", DefaultWorktree,
			FormatConventional, "{type}: {subject}", "{title}",
			"## Summary\n\n{title}\n\nBranch `{branch}` · task `{slug}`"),
		mk("ticket", "<TICKET>/<slug> branches, \"TICKET: subject\" commits", "{ticket}/{slug}", "{ticket}/{member}",
			FormatTicket, "{ticket}: {subject}", "[{ticket}] {title}",
			"## {ticket}: {title}\n\n### Changes\n\n### Testing\n\nBranch `{branch}`"),
		mk("personal", "<user>/<slug> branches, free-form commits", "{user}/{slug}", DefaultWorktree,
			FormatFree, "", "{title}", "Created from DHI worktree `{branch}`."),
	}
}

// Lookup finds a template by name: a user template first, then a
// built-in. builtin reports which.
func (c Config) Lookup(name string) (t Template, builtin, ok bool) {
	if t, ok := c.Templates[name]; ok {
		return t, false, true
	}
	for _, b := range Builtins() {
		if b.Name == name {
			return b.Template, true, true
		}
	}
	return Template{}, false, false
}

// TemplateNames lists built-ins in order, then user templates sorted.
func (c Config) TemplateNames() []string {
	var out []string
	for _, b := range Builtins() {
		if _, shadowed := c.Templates[b.Name]; !shadowed {
			out = append(out, b.Name)
		}
	}
	var user []string
	for n := range c.Templates {
		user = append(user, n)
	}
	sort.Strings(user)
	return append(out, user...)
}

// Apply copies t's non-empty fields into the flat rules.
func (c *Config) Apply(t Template) {
	set := func(dst *string, v string) {
		if strings.TrimSpace(v) != "" {
			*dst = v
		}
	}
	set(&c.Branch.Task, t.Branch.Task)
	set(&c.Branch.Review, t.Branch.Review)
	set(&c.Worktree.Task, t.Worktree.Task)
	set(&c.Commit.Format, t.Commit.Format)
	set(&c.Commit.TicketPattern, t.Commit.TicketPattern)
	c.Commit.Subject = t.Commit.Subject // "" is a real choice: free subjects
	set(&c.PR.Title, t.PR.Title)
	set(&c.PR.Body, t.PR.Body)
}

// Activate applies the named template and records it as active.
func (c *Config) Activate(name string) error {
	t, _, ok := c.Lookup(name)
	if !ok {
		return fmt.Errorf("conventions: no template %q", name)
	}
	c.Apply(t)
	c.Active = name
	return nil
}

// Snapshot captures the flat rules as a template (to copy and edit).
func (c Config) Snapshot() Template {
	t := Template{Branch: c.Branch, Worktree: c.Worktree, PR: c.PR}
	t.Commit.Format, t.Commit.TicketPattern, t.Commit.Subject = c.Commit.Format, c.Commit.TicketPattern, c.Commit.Subject
	return t
}

// TicketFor finds a ticket key for a task: the first match of the
// ticket pattern in its slug (upper-cased), title or labels; "" when
// none — callers then fall back to the slug.
func TicketFor(pattern string, texts ...string) string {
	re, err := regexp.Compile(pattern)
	if err != nil || pattern == "" {
		return ""
	}
	for _, t := range texts {
		if m := re.FindString(strings.ToUpper(t)); m != "" {
			return m
		}
	}
	return ""
}

// ExpandWorktree fills a worktree folder pattern into a clean relative
// path below the tasks directory. Each placeholder value is made
// path-safe; the result may not climb out, be absolute or be empty.
func ExpandWorktree(pattern string, vars map[string]string) (string, error) {
	if !strings.Contains(pattern, "{member}") {
		return "", fmt.Errorf("conventions: worktree pattern %q must contain {member}", pattern)
	}
	var bad string
	out := placeholderRe.ReplaceAllStringFunc(pattern, func(m string) string {
		k := m[1 : len(m)-1]
		v, ok := vars[k]
		if !ok {
			bad = k
			return m
		}
		return segmentBadRe.ReplaceAllString(strings.Trim(segmentBadRe.ReplaceAllString(v, "-"), "-"), "-")
	})
	if bad != "" {
		return "", fmt.Errorf("conventions: worktree pattern %q: unknown placeholder {%s}", pattern, bad)
	}
	clean := path.Clean(out)
	switch {
	case out == "" || clean == "." || strings.HasPrefix(out, "/") || strings.HasPrefix(clean, ".."):
		return "", fmt.Errorf("conventions: worktree pattern %q gives %q: must be a folder inside the tasks directory", pattern, out)
	case strings.Contains(out, "//") || strings.Contains(out, ".."):
		return "", fmt.Errorf("conventions: worktree pattern %q gives %q: empty or dot-dot segment", pattern, out)
	}
	return clean, nil
}

// ValidateWorktreePattern checks a pattern with representative values.
func ValidateWorktreePattern(pattern string) error {
	_, err := ExpandWorktree(pattern, SampleVars())
	return err
}

// SampleVars are the representative values previews and validation use.
func SampleVars() map[string]string {
	return map[string]string{
		"slug": "login-timeout", "id": "42", "user": "dana", "date": "20261013",
		"member": "api", "ticket": "PROJ-123", "title": "Fix login timeout", "type": "fix",
		"subject": "retry the token refresh", "branch": "task/login-timeout", "assignee": "forge",
	}
}

// Preview renders every rule of c for the sample task — what Settings
// shows while a template is chosen or edited.
func (c Config) Preview() []string {
	v := SampleVars()
	branch, err := ExpandBranch(c.Branch.Task, v)
	if err != nil {
		branch = "✗ " + err.Error()
	}
	v["branch"] = branch
	wt, err := ExpandWorktree(c.Worktree.Task, v)
	if err != nil {
		wt = "✗ " + err.Error()
	}
	subject := "retry the token refresh"
	switch {
	case c.Commit.Subject != "":
		subject = Render(c.Commit.Subject, v)
	case c.Commit.Format == FormatConventional:
		subject = "fix: retry the token refresh"
	case c.Commit.Format == FormatTicket:
		subject = "PROJ-123: retry the token refresh"
	}
	return []string{
		"branch    " + branch,
		"worktree  .dhi/tasks/" + wt,
		"commit    " + subject,
		"PR title  " + Render(c.PR.Title, v),
		"PR body   " + strings.ReplaceAll(Render(c.PR.Body, v), "\n", " ⏎ "),
	}
}
