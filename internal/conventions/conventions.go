// Package conventions holds the team-style rules DHI applies to what its
// agents produce: branch names, commit messages, source headers, PR
// text. It is pure (stdlib only) so settings can embed it and any
// service can call it. Every field has a default, so a workspace that
// configures nothing still gets sensible output (F-042).
package conventions

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Commit message formats.
const (
	FormatFree         = "free"         // any non-empty message
	FormatConventional = "conventional" // type(scope): subject
	FormatTicket       = "ticket"       // KEY-123: subject
)

// Config is the conventions schema; Defaults() is the baseline.
type Config struct {
	Branch    Branch    `toml:"branch"`
	Commit    Commit    `toml:"commit"`
	Copyright Copyright `toml:"copyright"`
	PR        PR        `toml:"pr"`
}

// Branch names task and review branches. Placeholders: {slug}, {id},
// {user}, {date}.
type Branch struct {
	Task   string `toml:"task"`
	Review string `toml:"review"`
}

// Commit configures message shape. Format is one of the Format*
// constants. TicketPattern (a regexp) is used by FormatTicket.
type Commit struct {
	Format        string `toml:"format"`
	TicketPattern string `toml:"ticket_pattern"`
	MaxSubject    int    `toml:"max_subject"`
	CoAuthor      string `toml:"co_author"` // "Name <email>" trailer; empty = none
}

// Copyright configures the header agents put on new source files.
type Copyright struct {
	Enabled bool   `toml:"enabled"`
	Holder  string `toml:"holder"`
	License string `toml:"license"` // SPDX id, e.g. "MIT"; empty = holder line only
	Year    string `toml:"year"`    // "" = current year at write time
}

// PR templates; {title} {slug} {summary} are substituted.
type PR struct {
	Title string `toml:"title"`
	Body  string `toml:"body"`
}

// Defaults is the built-in baseline every layer merges onto.
func Defaults() Config {
	return Config{
		Branch: Branch{Task: "task/{slug}", Review: "review/{id}"},
		Commit: Commit{
			Format:        FormatFree,
			TicketPattern: `[A-Z][A-Z0-9]+-\d+`,
			MaxSubject:    72,
		},
		PR: PR{Title: "{title}", Body: "{summary}"},
	}
}

var (
	placeholderRe  = regexp.MustCompile(`\{([a-z]+)\}`)
	branchBadRe    = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)
	conventionalRe = regexp.MustCompile(
		`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: \S`)
)

// ExpandBranch fills a branch pattern. Unknown placeholders and results
// git would reject are errors naming the pattern — no silent fallback.
func ExpandBranch(pattern string, vars map[string]string) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("conventions: empty branch pattern")
	}
	var bad string
	out := placeholderRe.ReplaceAllStringFunc(pattern, func(m string) string {
		k := m[1 : len(m)-1]
		v, ok := vars[k]
		if !ok {
			bad = k
			return m
		}
		return sanitizeRef(v)
	})
	if bad != "" {
		return "", fmt.Errorf("conventions: branch pattern %q: unknown placeholder {%s}", pattern, bad)
	}
	if err := checkRef(out); err != nil {
		return "", fmt.Errorf("conventions: branch pattern %q gives %q: %w", pattern, out, err)
	}
	return out, nil
}

func sanitizeRef(s string) string {
	return strings.Trim(branchBadRe.ReplaceAllString(s, "-"), "-.")
}

func checkRef(ref string) error {
	switch {
	case ref == "" || strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/"):
		return fmt.Errorf("empty or slash-bounded ref")
	case strings.Contains(ref, "//") || strings.Contains(ref, ".."):
		return fmt.Errorf("contains // or ..")
	case strings.HasSuffix(ref, ".lock") || strings.HasSuffix(ref, "."):
		return fmt.Errorf("bad suffix")
	}
	return nil
}

// ValidateBranchPattern reports whether a pattern can produce a valid
// ref (checked with representative values).
func ValidateBranchPattern(pattern string) error {
	_, err := ExpandBranch(pattern, map[string]string{
		"slug": "x", "id": "1", "user": "u", "date": "20260101"})
	return err
}

// Validate rejects a malformed Config by key name.
func (c Config) Validate() error {
	for key, p := range map[string]string{"branch.task": c.Branch.Task, "branch.review": c.Branch.Review} {
		if err := ValidateBranchPattern(p); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	switch c.Commit.Format {
	case FormatFree, FormatConventional, FormatTicket:
	default:
		return fmt.Errorf("commit.format %q (want %s, %s or %s)",
			c.Commit.Format, FormatFree, FormatConventional, FormatTicket)
	}
	if _, err := regexp.Compile(c.Commit.TicketPattern); err != nil {
		return fmt.Errorf("commit.ticket_pattern: %w", err)
	}
	if c.Commit.MaxSubject < 20 || c.Commit.MaxSubject > 200 {
		return fmt.Errorf("commit.max_subject %d out of range 20..200", c.Commit.MaxSubject)
	}
	if ca := strings.TrimSpace(c.Commit.CoAuthor); ca != "" && !coAuthorRe.MatchString(ca) {
		return fmt.Errorf("commit.co_author %q must look like \"Name <email>\"", ca)
	}
	if c.Copyright.Enabled && strings.TrimSpace(c.Copyright.Holder) == "" {
		return fmt.Errorf("copyright.holder is required when copyright.enabled")
	}
	return nil
}

var coAuthorRe = regexp.MustCompile(`^[^<>]+ <[^<>@\s]+@[^<>@\s]+>$`)

// CheckCommit validates a message against the configured format and
// returns an error that tells the author how to fix it.
func (c Commit) CheckCommit(msg string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Errorf("commit message is required")
	}
	subject, _, _ := strings.Cut(msg, "\n")
	if c.MaxSubject > 0 && len([]rune(subject)) > c.MaxSubject {
		return fmt.Errorf("commit subject is %d chars; limit is %d", len([]rune(subject)), c.MaxSubject)
	}
	switch c.Format {
	case FormatConventional:
		if !conventionalRe.MatchString(subject) {
			return fmt.Errorf("commit subject %q must be conventional: type(scope): subject (types: feat fix docs style refactor perf test build ci chore revert)", subject)
		}
	case FormatTicket:
		re, err := regexp.Compile(`^` + c.TicketPattern + `[:\s]`)
		if err != nil {
			return fmt.Errorf("commit.ticket_pattern: %w", err)
		}
		if !re.MatchString(subject) {
			return fmt.Errorf("commit subject %q must start with a ticket key matching %s, e.g. \"PROJ-123: subject\"", subject, c.TicketPattern)
		}
	}
	return nil
}

// Finalize applies the co-author trailer (once) to a validated message.
func (c Commit) Finalize(msg string) string {
	msg = strings.TrimRight(msg, " \n\t")
	if ca := strings.TrimSpace(c.CoAuthor); ca != "" {
		trailer := "Co-Authored-By: " + ca
		if !strings.Contains(msg, trailer) {
			msg += "\n\n" + trailer
		}
	}
	return msg + "\n"
}

// HeaderFor renders the copyright header for a source file name, or ""
// when disabled or the language has no comment syntax we know. now
// supplies the year when Year is unset.
func (c Copyright) HeaderFor(filename string, now time.Time) string {
	if !c.Enabled {
		return ""
	}
	open, line, closer, ok := commentStyle(filename)
	if !ok {
		return ""
	}
	year := c.Year
	if year == "" {
		year = fmt.Sprint(now.Year())
	}
	lines := []string{fmt.Sprintf("Copyright (c) %s %s", year, c.Holder)}
	if c.License != "" {
		lines = append(lines, "SPDX-License-Identifier: "+c.License)
	}
	var b strings.Builder
	if open != "" {
		b.WriteString(open + "\n")
	}
	for _, l := range lines {
		b.WriteString(line + l + "\n")
	}
	if closer != "" {
		b.WriteString(closer + "\n")
	}
	return b.String()
}

// EnsureHeader prepends the header to content when missing. A shebang
// line stays first. Content already carrying a "Copyright" line in its
// first 5 lines is returned untouched.
func (c Copyright) EnsureHeader(filename, content string, now time.Time) string {
	h := c.HeaderFor(filename, now)
	if h == "" {
		return content
	}
	head := strings.SplitN(content, "\n", 6)
	for i := 0; i < len(head) && i < 5; i++ {
		if strings.Contains(head[i], "Copyright") {
			return content
		}
	}
	if strings.HasPrefix(content, "#!") {
		first, rest, _ := strings.Cut(content, "\n")
		return first + "\n" + h + "\n" + rest
	}
	return h + "\n" + content
}

func commentStyle(name string) (open, line, closer string, ok bool) {
	ext := name
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = name[i:]
	}
	switch strings.ToLower(ext) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".java", ".kt", ".swift", ".rs", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".scala", ".dart":
		return "", "// ", "", true
	case ".py", ".sh", ".rb", ".toml", ".yaml", ".yml", ".pl", ".r":
		return "", "# ", "", true
	case ".css", ".scss":
		return "/*", " * ", " */", true
	case ".html", ".xml", ".svg":
		return "<!--", "  ", "-->", true
	case ".sql", ".lua":
		return "", "-- ", "", true
	}
	return "", "", "", false
}

// Render substitutes {name} placeholders in a template; unknown names
// are left intact so a typo is visible in the output.
func Render(tpl string, vars map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		if v, ok := vars[m[1:len(m)-1]]; ok {
			return v
		}
		return m
	})
}

// Guidance renders the rules as an instruction block for an agent's
// system prompt, so it follows them before DHI's gates have to refuse.
func (c Config) Guidance() string {
	var b strings.Builder
	b.WriteString("Team conventions (enforced by DHI):\n")
	fmt.Fprintf(&b, "- Branches: tasks use %q, reviews use %q.\n", c.Branch.Task, c.Branch.Review)
	switch c.Commit.Format {
	case FormatConventional:
		b.WriteString("- Commit subjects follow Conventional Commits: type(scope): subject.\n")
	case FormatTicket:
		fmt.Fprintf(&b, "- Commit subjects start with a ticket key matching %s, e.g. \"PROJ-123: subject\".\n", c.Commit.TicketPattern)
	default:
		b.WriteString("- Commit messages: a short imperative subject, then a body explaining why.\n")
	}
	fmt.Fprintf(&b, "- Keep commit subjects within %d characters.\n", c.Commit.MaxSubject)
	if c.Commit.CoAuthor != "" {
		b.WriteString("- DHI adds the Co-Authored-By trailer to commits; do not add it yourself.\n")
	}
	if c.Copyright.Enabled {
		b.WriteString("- New source files begin with the copyright header: ")
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(c.Copyright.HeaderFor("x.go", time.Now()), "\n", " ")))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
