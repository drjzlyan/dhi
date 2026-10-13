package conventions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Inference is what DHI learned about a team's commit and PR style
// (F-082), from git history, the repo's PR template, or an example the
// user pasted. Empty fields mean "nothing learned".
type Inference struct {
	Format        string
	TicketPattern string
	Subject       string
	MaxSubject    int
	Body          string // commit body example (structure, not words)
	PRBody        string // PR description template
	Evidence      []string
}

// Empty reports whether nothing was learned.
func (i Inference) Empty() bool {
	return i.Format == "" && i.Subject == "" && i.MaxSubject == 0 && i.Body == "" && i.PRBody == ""
}

// ticketLeadRe finds a ticket key leading a subject, with its separator.
var ticketLeadRe = regexp.MustCompile(`^([A-Z][A-Z0-9]+-\d+)(:?)\s`)

// InferCommits reads commit subjects (newest first) and infers the
// format when a clear majority (60%) agrees, and a subject limit that
// fits the history. Fewer than 5 subjects infer nothing.
func InferCommits(subjects []string) Inference {
	var subs []string
	for _, s := range subjects {
		if s = strings.TrimSpace(s); s != "" && !strings.HasPrefix(s, "Merge ") {
			subs = append(subs, s)
		}
	}
	n := len(subs)
	if n < 5 {
		return Inference{}
	}
	conv, ticket, colon := 0, 0, 0
	var lens []int
	for _, s := range subs {
		lens = append(lens, len([]rune(s)))
		if conventionalRe.MatchString(s) {
			conv++
		}
		if m := ticketLeadRe.FindStringSubmatch(s); m != nil {
			ticket++
			if m[2] == ":" {
				colon++
			}
		}
	}
	var inf Inference
	switch {
	case conv*10 >= n*6:
		inf.Format = FormatConventional
		inf.Evidence = append(inf.Evidence, fmt.Sprintf("%d of %d recent commits use Conventional Commits", conv, n))
	case ticket*10 >= n*6:
		inf.Format = FormatTicket
		inf.TicketPattern = `[A-Z][A-Z0-9]+-\d+`
		inf.Subject = "{ticket} {subject}"
		if colon*2 >= ticket {
			inf.Subject = "{ticket}: {subject}"
		}
		inf.Evidence = append(inf.Evidence, fmt.Sprintf("%d of %d recent commits start with a ticket key", ticket, n))
	default:
		inf.Format = FormatFree
		inf.Evidence = append(inf.Evidence, fmt.Sprintf("no single format in %d recent commits: free-form", n))
	}
	sort.Ints(lens)
	p95 := lens[max(0, (len(lens)*95+99)/100-1)]
	inf.MaxSubject = 72
	if p95 > 72 {
		inf.MaxSubject = min((p95+9)/10*10, 120)
		inf.Evidence = append(inf.Evidence, fmt.Sprintf("subjects run long: limit %d characters", inf.MaxSubject))
	}
	return inf
}

// trailerRe matches git trailers DHI or tools add on their own.
var trailerRe = regexp.MustCompile(`(?i)^(co-authored-by|signed-off-by|reviewed-by|change-id):`)

// InferFromExample learns from one commit message and/or PR description
// the user likes. The commit's subject decides the format; its body
// (trailers removed) becomes the example agents follow. The PR text
// becomes the PR template, reduced to its skeleton when it has headings.
func InferFromExample(commit, pr string) Inference {
	var inf Inference
	commit = strings.TrimSpace(strings.ReplaceAll(commit, "\r\n", "\n"))
	if commit != "" {
		subject, body, _ := strings.Cut(commit, "\n")
		subject = strings.TrimSpace(subject)
		switch {
		case conventionalRe.MatchString(subject):
			inf.Format = FormatConventional
			inf.Evidence = append(inf.Evidence, "subject is a Conventional Commit")
		case ticketLeadRe.MatchString(subject):
			m := ticketLeadRe.FindStringSubmatch(subject)
			inf.Format, inf.TicketPattern = FormatTicket, `[A-Z][A-Z0-9]+-\d+`
			inf.Subject = "{ticket}" + m[2] + " {subject}"
			inf.Evidence = append(inf.Evidence, "subject starts with a ticket key ("+m[1]+")")
		default:
			inf.Format = FormatFree
			inf.Evidence = append(inf.Evidence, "subject has no fixed prefix: free-form")
		}
		if l := len([]rune(subject)); l > 72 {
			inf.MaxSubject = min((l+9)/10*10, 120)
		}
		var keep []string
		for _, l := range strings.Split(body, "\n") {
			if !trailerRe.MatchString(strings.TrimSpace(l)) {
				keep = append(keep, strings.TrimRight(l, " \t"))
			}
		}
		if b := strings.TrimSpace(strings.Join(keep, "\n")); b != "" {
			inf.Body = b
			inf.Evidence = append(inf.Evidence, "commit bodies follow your example's structure")
		}
	}
	if pr = strings.TrimSpace(strings.ReplaceAll(pr, "\r\n", "\n")); pr != "" {
		inf.PRBody = PRSkeleton(pr)
		inf.Evidence = append(inf.Evidence, "PR descriptions use your example's sections")
	}
	return inf
}

// PRSkeleton keeps a PR description's structure: headings, checklist
// items (unchecked) and HTML comments (template hints). Text without
// headings is kept whole. Braces are escaped out so it is a valid
// template; {title} heads it when it has no heading of its own.
func PRSkeleton(text string) string {
	text = strings.NewReplacer("{", "(", "}", ")").Replace(strings.TrimSpace(text))
	var keep []string
	headings := 0
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "#"):
			headings++
			if len(keep) > 0 {
				keep = append(keep, "")
			}
			keep = append(keep, t, "")
		case strings.HasPrefix(t, "- [ ]") || strings.HasPrefix(t, "- [x]") || strings.HasPrefix(t, "- [X]"):
			keep = append(keep, "- [ ] "+strings.TrimSpace(t[5:]))
		case strings.HasPrefix(t, "<!--"):
			keep = append(keep, t)
		}
	}
	if headings == 0 {
		return text
	}
	return strings.TrimSpace(strings.Join(collapseBlank(keep), "\n"))
}

func collapseBlank(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// prTemplatePaths are where GitHub looks for a PR template.
var prTemplatePaths = []string{
	".github/pull_request_template.md", ".github/PULL_REQUEST_TEMPLATE.md",
	"pull_request_template.md", "PULL_REQUEST_TEMPLATE.md",
	"docs/pull_request_template.md", "docs/PULL_REQUEST_TEMPLATE.md",
}

// PRTemplate returns the repo's PR template made safe as a pr.body
// ("" when there is none). A template directory uses its first file.
func PRTemplate(dir string) string {
	for _, p := range prTemplatePaths {
		if b, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
			return strings.NewReplacer("{", "(", "}", ")").Replace(strings.TrimSpace(string(b)))
		}
	}
	if m, _ := filepath.Glob(filepath.Join(dir, ".github", "PULL_REQUEST_TEMPLATE", "*.md")); len(m) > 0 {
		sort.Strings(m)
		if b, err := os.ReadFile(m[0]); err == nil {
			return strings.NewReplacer("{", "(", "}", ")").Replace(strings.TrimSpace(string(b)))
		}
	}
	return ""
}

// Learn applies what was inferred onto c; empty fields keep c's rules.
func (c *Config) Learn(i Inference) {
	if i.Format != "" {
		c.Commit.Format = i.Format
		if i.Format != FormatTicket {
			c.Commit.Subject = ""
		}
	}
	if i.TicketPattern != "" {
		c.Commit.TicketPattern = i.TicketPattern
	}
	if i.Subject != "" {
		c.Commit.Subject = i.Subject
	}
	if i.MaxSubject != 0 {
		c.Commit.MaxSubject = i.MaxSubject
	}
	if i.Body != "" {
		c.Commit.Body = i.Body
	}
	if i.PRBody != "" {
		c.PR.Body = i.PRBody
	}
}
