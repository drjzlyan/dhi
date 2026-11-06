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
	Worktree  Worktree  `toml:"worktree"`
	Commit    Commit    `toml:"commit"`
	Copyright Copyright `toml:"copyright"`
	PR        PR        `toml:"pr"`

	// Templates are the team's named naming schemes (F-075); Active names
	// the one last applied — built-in or from Templates. Applying a
	// template copies its fields into the flat rules above, so every
	// consumer reads the flat rules and nothing else.
	Templates map[string]Template `toml:"templates,omitempty"`
	Active    string              `toml:"active,omitempty"`
}

// Worktree names task worktree folders under the tasks directory
// (F-075). Placeholders: {slug}, {member}, {ticket}, {user}, {date};
// {member} is required so a multi-repo card never collides.
type Worktree struct {
	Task string `toml:"task"`
}

// Branch names task and review branches. Symbols per FieldSymbols; a
// task branch may hold {ai:…} slots (F-182), a review branch may not.
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
	// CoAuthor is the "Name <email>" credited in a Co-Authored-By trailer.
	// It has no default (DHI drives several CLIs; crediting one by default
	// would misattribute the others), and is applied only when
	// CoAuthorEnabled is set.
	CoAuthor        string `toml:"co_author"`
	CoAuthorEnabled bool   `toml:"co_author_enabled"`
	// Subject is the commit subject shape agents follow and the human's
	// commit form starts from (F-075), e.g. "{ticket}: {subject}".
	// Placeholders: {ticket}, {slug}, {type}, {subject}. "" = free.
	Subject string `toml:"subject,omitempty"`
	// Body is an example commit body (F-082): agents follow its
	// structure and style, not its words. "" = a body that explains why.
	Body string `toml:"body,omitempty"`
	// BodyFormat is a commit body template (F-182): symbols DHI fills and
	// {ai:…} slots the author writes, enforced like Subject. "" = none.
	BodyFormat string `toml:"body_format,omitempty"`
}

// Copyright configures the header agents put on new source files.
type Copyright struct {
	Enabled bool   `toml:"enabled"`
	Holder  string `toml:"holder"`
	License string `toml:"license"` // SPDX id, e.g. "MIT"; empty = holder line only
	Year    string `toml:"year"`    // "" = current year at write time
}

// PR templates, applied to every PR DHI opens (pr_open, the board, the
// reviewer): Title wraps the title the author gave as {title}; Body is
// the description. Both take the message symbols and {ai:…} slots
// (F-182). Unknown placeholders are refused, not silently kept.
type PR struct {
	Title string `toml:"title"`
	Body  string `toml:"body"`
}

// Defaults is the built-in baseline every layer merges onto.
func Defaults() Config {
	return Config{
		Branch:   Branch{Task: "task/{slug}", Review: "review/{id}"},
		Worktree: Worktree{Task: DefaultWorktree},
		Commit: Commit{
			Format:        FormatFree,
			TicketPattern: `[A-Z][A-Z0-9]+-\d+`,
			MaxSubject:    72,
		},
		PR: PR{Title: "{title}", Body: "Created from DHI worktree `{branch}`."},
	}
}

var (
	branchBadRe    = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)
	conventionalRe = regexp.MustCompile(
		`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: \S`)
)

// ExpandBranch fills a branch pattern that has no AI slots. Unknown
// placeholders and results git would reject are errors naming the
// pattern — no silent fallback.
func ExpandBranch(pattern string, vars map[string]string) (string, error) {
	return ExpandBranchFill(pattern, vars, nil)
}

// ExpandBranchFill fills a branch pattern, AI slots from fill. Every
// value is made ref-safe; a missing AI text is a *MissingAIError.
func ExpandBranchFill(pattern string, vars, fill map[string]string) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("conventions: empty branch pattern")
	}
	out, err := render(pattern, vars, fill, func(v string) string { return sanitizeRef(capRunes(v, maxBranchPart)) })
	if err != nil {
		if _, ok := IsMissingAI(err); ok {
			return "", err
		}
		return "", fmt.Errorf("conventions: branch pattern %q: %w", pattern, err)
	}
	if err := checkRef(out); err != nil {
		return "", fmt.Errorf("conventions: branch pattern %q gives %q: %w", pattern, out, err)
	}
	return out, nil
}

// maxBranchPart caps one filled value in a branch name, so an AI-written
// part stays a name, not a sentence.
const maxBranchPart = 48

func capRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimRight(string(r[:n]), "-_. ")
	}
	return s
}

func sanitizeRef(s string) string {
	return strings.Trim(branchBadRe.ReplaceAllString(s, "-"), "-.")
}

func checkRef(ref string) error {
	switch {
	case ref == "" || strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/"):
		return fmt.Errorf("empty or slash-bounded ref")
	case strings.Contains(ref, "//") || strings.Contains(ref, ".."):
		return fmt.Errorf("contains a double slash or dot-dot")
	case strings.HasSuffix(ref, ".lock") || strings.HasSuffix(ref, "."):
		return fmt.Errorf("bad suffix")
	}
	return nil
}

// ValidateBranchPattern reports whether a task-branch pattern can produce
// a valid ref (its symbols checked, then filled with representative
// values and sample AI text).
func ValidateBranchPattern(pattern string) error { return validateBranch(FieldBranchTask, pattern) }

func validateBranch(field, pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("conventions: empty branch pattern")
	}
	if err := ValidateField(field, pattern); err != nil {
		return err
	}
	_, err := ExpandBranchFill(pattern, SampleVars(), sampleFill(pattern))
	return err
}

// sampleFill gives every AI slot of tpl representative text.
func sampleFill(tpl string) map[string]string {
	fill := map[string]string{}
	for _, sl := range Slots(tpl) {
		fill[sl.Key] = SampleAI(sl.Key)
	}
	return fill
}

// SampleAI is the representative AI text previews show for a slot.
func SampleAI(key string) string {
	switch key {
	case "type":
		return "fix"
	case "subject":
		return "retry the token refresh"
	}
	return "retry token refresh"
}

// Validate rejects a malformed Config by key name.
func (c Config) Validate() error {
	if err := validateBranch(FieldBranchTask, c.Branch.Task); err != nil {
		return fmt.Errorf("branch.task: %w", err)
	}
	if err := validateBranch(FieldBranchReview, c.Branch.Review); err != nil {
		return fmt.Errorf("branch.review: %w", err)
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
	if c.Commit.CoAuthorEnabled && strings.TrimSpace(c.Commit.CoAuthor) == "" {
		return fmt.Errorf("commit.co_author is required when commit.co_author_enabled")
	}
	if c.Copyright.Enabled && strings.TrimSpace(c.Copyright.Holder) == "" {
		return fmt.Errorf("copyright.holder is required when copyright.enabled")
	}
	for field, tpl := range map[string]string{FieldPRTitle: c.PR.Title, FieldPRBody: c.PR.Body,
		FieldCommitSubject: c.Commit.Subject, FieldCommitBody: c.Commit.BodyFormat} {
		if err := ValidateField(field, tpl); err != nil {
			return err
		}
	}
	if err := ValidateWorktreePattern(c.Worktree.Task); err != nil {
		return fmt.Errorf("worktree.task: %w", err)
	}
	for name, t := range c.Templates {
		if !templateNameRe.MatchString(name) {
			return fmt.Errorf("templates.%s: name must be lowercase letters, digits and dashes", name)
		}
		probe := Defaults()
		probe.Apply(t)
		probe.Templates, probe.Active = nil, ""
		if err := probe.Validate(); err != nil {
			return fmt.Errorf("templates.%s.%w", name, err)
		}
	}
	return nil
}

var coAuthorRe = regexp.MustCompile(`^[^<>]+ <[^<>@\s]+@[^<>@\s]+>$`)

// CheckCommit validates a message against the configured format and
// templates, knowing no task values; see CheckCommitFor.
func (c Commit) CheckCommit(msg string) error { return c.CheckCommitFor(msg, nil) }

// CheckCommitFor validates a message against the configured format, the
// subject template and the body format, with the symbols of the task it
// is for (F-182): fixed parts must read as rendered, AI slots take any
// text. The error tells the author how to fix it.
func (c Commit) CheckCommitFor(msg string, vars map[string]string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Errorf("commit message is required")
	}
	subject, body, _ := strings.Cut(msg, "\n")
	if c.MaxSubject > 0 && len([]rune(subject)) > c.MaxSubject {
		return fmt.Errorf("commit subject is %d chars; limit is %d", len([]rune(subject)), c.MaxSubject)
	}
	switch c.Format {
	case FormatConventional:
		if !conventionalRe.MatchString(subject) {
			return fmt.Errorf("commit subject %q must be conventional: type(scope): subject (types: feat fix docs style refactor perf test build ci chore revert)", subject)
		}
	case FormatTicket:
		if c.noTicketKey(vars) {
			break // the subject template's shape, with the slug, decides
		}
		re, err := regexp.Compile(`^` + c.TicketPattern + `[:\s]`)
		if err != nil {
			return fmt.Errorf("commit.ticket_pattern: %w", err)
		}
		if !re.MatchString(subject) {
			return fmt.Errorf("commit subject %q must start with a ticket key matching %s, e.g. \"PROJ-123: subject\"", subject, c.TicketPattern)
		}
	}
	if err := Match(FieldCommitSubject, c.Subject, vars, subject); err != nil {
		return fmt.Errorf("%w; got %q", err, subject)
	}
	if strings.TrimSpace(c.BodyFormat) != "" {
		if err := Match(FieldCommitBody, c.BodyFormat, vars, stripTrailers(body)); err != nil {
			return err
		}
	}
	return nil
}

// noTicketKey reports a task without a ticket key under a subject
// template that places {ticket}: DHI fills the slug there, so asking for
// a key the card does not have would refuse every commit.
func (c Commit) noTicketKey(vars map[string]string) bool {
	t, ok := vars["ticket"]
	if !ok || !strings.Contains(c.Subject, "{ticket") {
		return false
	}
	re, err := regexp.Compile(`^(?:` + c.TicketPattern + `)$`)
	return err == nil && !re.MatchString(t)
}

// ComposeCommit renders a whole message from the templates: the subject
// template (required) and, when set, the body format. body is the free
// body used when there is no body format.
func (c Commit) ComposeCommit(vars, fill map[string]string, body string) (string, error) {
	subjectTpl := c.Subject
	if strings.TrimSpace(subjectTpl) == "" {
		if strings.TrimSpace(c.BodyFormat) == "" {
			return "", fmt.Errorf("the team has no commit template to fill: write the whole message")
		}
		subjectTpl = "{subject}" // a body format alone: the subject is a part you write
	}
	subject, err := Render(subjectTpl, vars, fill)
	if err != nil {
		return "", fmt.Errorf("commit subject: %w", err)
	}
	if strings.TrimSpace(c.BodyFormat) != "" {
		if body, err = Render(c.BodyFormat, vars, fill); err != nil {
			return "", fmt.Errorf("commit body: %w", err)
		}
	}
	msg := strings.TrimSpace(subject)
	if b := strings.TrimSpace(body); b != "" {
		msg += "\n\n" + b
	}
	return msg, nil
}

// stripTrailers drops the git trailers (Co-Authored-By: …) closing a
// body, which DHI or the author's tools add outside the body format.
func stripTrailers(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n "), "\n")
	for len(lines) > 0 && trailerRe.MatchString(strings.TrimSpace(lines[len(lines)-1])) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// Finalize applies the co-author trailer (once) to a validated message.
func (c Commit) Finalize(msg string) string {
	msg = strings.TrimRight(msg, " \n\t")
	if ca := strings.TrimSpace(c.CoAuthor); c.CoAuthorEnabled && ca != "" {
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

// Guidance renders the rules as an instruction block for an agent's
// system prompt, so it follows them before DHI's gates have to refuse.
func (c Config) Guidance() string { return c.GuidanceFor(nil) }

// GuidanceFor is Guidance for one task: each template is shown with the
// symbols DHI knows already filled and the AI slots the agent writes
// marked <like this>, so every CLI gets the same exact shapes (F-182).
func (c Config) GuidanceFor(vars map[string]string) string {
	var b strings.Builder
	b.WriteString("Team conventions (enforced by DHI):\n")
	fmt.Fprintf(&b, "- Branches: tasks use %q, reviews use %q.\n", Shape(c.Branch.Task, vars), Shape(c.Branch.Review, vars))
	switch c.Commit.Format {
	case FormatConventional:
		b.WriteString("- Commit subjects follow Conventional Commits: type(scope): subject.\n")
	case FormatTicket:
		if c.Commit.noTicketKey(vars) {
			b.WriteString("- This task has no ticket key: its slug stands in for it in commit subjects.\n")
		} else {
			fmt.Fprintf(&b, "- Commit subjects start with a ticket key matching %s, e.g. \"PROJ-123: subject\".\n", c.Commit.TicketPattern)
		}
	default:
		b.WriteString("- Commit messages: a short imperative subject, then a body explaining why.\n")
	}
	if c.Commit.Subject != "" {
		fmt.Fprintf(&b, "- Commit subjects take the shape %q.\n", Shape(c.Commit.Subject, vars))
	}
	if f := strings.TrimSpace(c.Commit.BodyFormat); f != "" {
		b.WriteString("- Commit bodies take this shape (text outside <…> exactly as shown):\n")
		indent(&b, Shape(f, vars))
	} else if body := strings.TrimSpace(c.Commit.Body); body != "" {
		b.WriteString("- Commit bodies follow the structure and style of this example (its sections and tone, not its words):\n")
		indent(&b, body)
	}
	fmt.Fprintf(&b, "- Keep commit subjects within %d characters.\n", c.Commit.MaxSubject)
	if c.Commit.CoAuthorEnabled {
		b.WriteString("- DHI adds the Co-Authored-By trailer to commits; do not add it yourself.\n")
	}
	if c.PR.Title != "" && c.PR.Title != "{title}" {
		fmt.Fprintf(&b, "- PR titles take the shape %q; pr_open's title is the {title} part.\n", Shape(c.PR.Title, vars))
	}
	if HasAI(c.PR.Body) {
		b.WriteString("- PR descriptions take this shape; DHI writes the fixed parts:\n")
		indent(&b, Shape(c.PR.Body, vars))
	}
	if slots := c.agentSlots(); len(slots) > 0 {
		b.WriteString("- Parts in <…> are yours to write. Pass them as `fill`, a list of {\"key\", \"text\"} with the keys below, to git_commit, pr_open or work_branch, and DHI puts them in place:\n")
		b.WriteString(SlotList(slots) + "\n")
	}
	if open := c.openSymbols(vars); len(open) > 0 {
		b.WriteString("- DHI fills the {symbols}: " + strings.Join(open, "; ") + ".\n")
	}
	b.WriteString("- Commit only with git_commit and open PRs only with pr_open: a `git commit` or `gh pr create` of your own skips these rules.\n")
	if c.Copyright.Enabled {
		b.WriteString("- New source files begin with the copyright header: ")
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(c.Copyright.HeaderFor("x.go", time.Now()), "\n", " ")))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// openSymbols explains the symbols left unfilled in the guidance (no
// task is bound yet), e.g. "{ticket} is the task's ticket key …".
func (c Config) openSymbols(vars map[string]string) []string {
	used := map[string]bool{}
	for _, tpl := range []string{c.Branch.Task, c.Branch.Review, c.Commit.Subject, c.Commit.BodyFormat, c.PR.Title, c.PR.Body} {
		toks, _ := parse(tpl)
		for _, t := range toks {
			if _, known := vars[t.text]; t.kind == tokVar && !known {
				used[t.text] = true
			}
		}
	}
	var out []string
	for _, s := range Symbols {
		if used[s.Name] {
			out = append(out, "{"+s.Name+"} = "+s.Desc)
		}
	}
	return out
}

// agentSlots are the AI slots of every template an agent fills, once
// per key, in reading order.
func (c Config) agentSlots() []Slot {
	var out []Slot
	seen := map[string]bool{}
	subject := c.Commit.Subject
	if strings.TrimSpace(subject) == "" && HasAI(c.Commit.BodyFormat) {
		subject = "{subject}" // ComposeCommit's subject for a body format alone
	}
	for _, tpl := range []string{c.Branch.Task, subject, c.Commit.BodyFormat, c.PR.Title, c.PR.Body} {
		for _, sl := range Slots(tpl) {
			if !seen[sl.Key] {
				seen[sl.Key] = true
				out = append(out, sl)
			}
		}
	}
	return out
}

func indent(b *strings.Builder, text string) {
	for _, l := range strings.Split(text, "\n") {
		b.WriteString("    " + l + "\n")
	}
}
