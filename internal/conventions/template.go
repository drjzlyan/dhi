package conventions

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The template language every naming and message rule shares (F-182).
//
//	{name}          a symbol DHI fills: {ticket}, {slug}, {title}, …
//	{name|kebab}    the same, through filters: lower, upper, kebab, snake
//	{ai}            text an AI writes
//	{ai:hint}       text an AI writes, described by hint (one key per hint)
//	{{ and }}       a literal brace
//
// Any other brace text stays literal, so prose and code samples in a PR
// body need no escaping. A well-formed {word} that is not a symbol of its
// field is refused by Validate, never silently kept.

// Fields, by the config key that holds each template.
const (
	FieldBranchTask    = "branch.task"
	FieldBranchReview  = "branch.review"
	FieldWorktree      = "worktree.task"
	FieldCommitSubject = "commit.subject"
	FieldCommitBody    = "commit.body_format"
	FieldPRTitle       = "pr.title"
	FieldPRBody        = "pr.body"
)

// Symbol is one name a template may use.
type Symbol struct{ Name, Desc string }

// Symbols are every symbol, in the order help lists them.
var Symbols = []Symbol{
	{"ticket", "the task's ticket key (PROJ-123), else its slug"},
	{"slug", "the task's short id (login-timeout)"},
	{"id", "the task slug, or the review id on a review branch"},
	{"title", "the task title; in a PR title, the title the author gave"},
	{"member", "the repo the change is in"},
	{"branch", "the branch being committed or opened as a PR"},
	{"assignee", "who the card is assigned to"},
	{"user", "your login name"},
	{"date", "today, as 20261105"},
}

// Filters change a value before it is placed.
var Filters = []Symbol{
	{"lower", "lower case"},
	{"upper", "UPPER CASE"},
	{"kebab", "lower-case-with-dashes"},
	{"snake", "lower_case_with_underscores"},
}

var (
	taskSyms   = []string{"ticket", "slug", "id", "title", "member", "assignee", "user", "date"}
	reviewSyms = []string{"id", "slug", "ticket", "user", "date"}
	msgSyms    = []string{"ticket", "slug", "id", "title", "member", "branch", "assignee", "user", "date"}
)

// fieldRule is what one field's templates may hold.
type fieldRule struct {
	label   string
	symbols []string
	ai      bool // {ai…} slots allowed
	legacy  bool // {type} and {subject} (commit subjects before F-182)
	multi   bool // multi-line text
}

var fieldRules = map[string]fieldRule{
	FieldBranchTask:    {label: "task branch", symbols: taskSyms, ai: true},
	FieldBranchReview:  {label: "review branch", symbols: reviewSyms},
	FieldWorktree:      {label: "worktree folder", symbols: taskSyms},
	FieldCommitSubject: {label: "commit subject", symbols: msgSyms, ai: true, legacy: true},
	FieldCommitBody:    {label: "commit body", symbols: msgSyms, ai: true, legacy: true, multi: true},
	FieldPRTitle:       {label: "PR title", symbols: msgSyms, ai: true},
	FieldPRBody:        {label: "PR body", symbols: msgSyms, ai: true, multi: true},
}

// FieldLabel names a field for people ("commit subject").
func FieldLabel(field string) string { return fieldRules[field].label }

// FieldSymbols lists the symbols a field may use, and whether it takes {ai}.
func FieldSymbols(field string) (symbols []string, ai bool) {
	r := fieldRules[field]
	return append([]string{}, r.symbols...), r.ai
}

// legacyHints describe the two agent-written placeholders commit
// subjects had before {ai}; they behave as {ai:type} and {ai:subject}.
var legacyHints = map[string]string{
	"type":    "Conventional Commits type: feat, fix, docs, refactor, test, chore, …",
	"subject": "imperative summary of the change",
}

type tokKind int

const (
	tokText tokKind = iota
	tokVar
	tokAI
)

type token struct {
	kind    tokKind
	text    string // literal text, or the symbol name
	key     string // AI slot key
	hint    string // AI slot description
	raw     string // the token as written
	filters []string
}

var tokenRe = regexp.MustCompile(`\{\{|\}\}|\{(ai(?::[^{}|]*)?|[a-z]+)((?:\|[a-z]+)*)\}`)

func parse(tpl string) ([]token, error) {
	var out []token
	text := func(s string) {
		if s == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].kind == tokText {
			out[n-1].text += s
			out[n-1].raw += s
			return
		}
		out = append(out, token{kind: tokText, text: s, raw: s})
	}
	last := 0
	for _, m := range tokenRe.FindAllStringSubmatchIndex(tpl, -1) {
		text(tpl[last:m[0]])
		last = m[1]
		whole := tpl[m[0]:m[1]]
		switch whole {
		case "{{":
			out = append(out, token{kind: tokText, text: "{", raw: whole})
			continue
		case "}}":
			out = append(out, token{kind: tokText, text: "}", raw: whole})
			continue
		}
		name := tpl[m[2]:m[3]]
		var filters []string
		if m[4] < m[5] {
			filters = strings.Split(tpl[m[4]+1:m[5]], "|")
		}
		for _, f := range filters {
			if !validFilter(f) {
				return nil, fmt.Errorf("unknown filter |%s in %s (filters: lower, upper, kebab, snake)", f, whole)
			}
		}
		t := token{kind: tokVar, text: name, raw: whole, filters: filters}
		switch {
		case name == "ai" || strings.HasPrefix(name, "ai:"):
			hint := strings.Join(strings.Fields(strings.TrimPrefix(strings.TrimPrefix(name, "ai"), ":")), " ")
			t.kind, t.hint, t.key = tokAI, hint, slotKey(hint)
		case legacyHints[name] != "":
			t.kind, t.hint, t.key = tokAI, legacyHints[name], name
		}
		out = append(out, t)
	}
	text(tpl[last:])
	return out, nil
}

func validFilter(f string) bool {
	for _, s := range Filters {
		if s.Name == f {
			return true
		}
	}
	return false
}

// slotKey is how a fill names an AI slot: its hint in lower case with
// single spaces, or "ai" for a bare {ai}.
func slotKey(hint string) string {
	if k := strings.ToLower(strings.Join(strings.Fields(hint), " ")); k != "" {
		return k
	}
	return "ai"
}

var nonWordRe = regexp.MustCompile(`[^a-z0-9]+`)

func applyFilters(v string, filters []string) string {
	for _, f := range filters {
		switch f {
		case "lower":
			v = strings.ToLower(v)
		case "upper":
			v = strings.ToUpper(v)
		case "kebab":
			v = strings.Trim(nonWordRe.ReplaceAllString(strings.ToLower(v), "-"), "-")
		case "snake":
			v = strings.Trim(nonWordRe.ReplaceAllString(strings.ToLower(v), "_"), "_")
		}
	}
	return v
}

// ValidateField checks a template against its field's rules: known
// filters, only the field's symbols, and {ai} only where AI may write.
func ValidateField(field, tpl string) error {
	r, ok := fieldRules[field]
	if !ok {
		return fmt.Errorf("conventions: unknown field %q", field)
	}
	toks, err := parse(tpl)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	allowed := map[string]bool{}
	for _, s := range r.symbols {
		allowed[s] = true
	}
	for _, t := range toks {
		switch t.kind {
		case tokVar:
			if !allowed[t.text] {
				return fmt.Errorf("%s: unknown placeholder {%s} (allowed: %s)", field, t.text, allowedList(r))
			}
		case tokAI:
			legacy := legacyHints[t.key] != "" && !strings.HasPrefix(t.raw, "{ai")
			switch {
			case legacy && !r.legacy:
				return fmt.Errorf("%s: unknown placeholder {%s} (allowed: %s)", field, t.key, allowedList(r))
			case !legacy && !r.ai:
				return fmt.Errorf("%s: {ai} is not allowed here: a %s must come out the same every time (allowed: %s)", field, r.label, allowedList(r))
			}
		}
	}
	return nil
}

func allowedList(r fieldRule) string {
	parts := make([]string, 0, len(r.symbols)+3)
	for _, s := range r.symbols {
		parts = append(parts, "{"+s+"}")
	}
	if r.legacy {
		parts = append(parts, "{type}", "{subject}")
	}
	if r.ai {
		parts = append(parts, "{ai:…}")
	}
	return strings.Join(parts, ", ")
}

// Slot is one AI-written part of a template.
type Slot struct{ Key, Hint string }

// Slots lists a template's AI slots, once per key, in order.
func Slots(tpl string) []Slot {
	toks, err := parse(tpl)
	if err != nil {
		return nil
	}
	var out []Slot
	seen := map[string]bool{}
	for _, t := range toks {
		if t.kind == tokAI && !seen[t.key] {
			seen[t.key] = true
			out = append(out, Slot{Key: t.key, Hint: t.hint})
		}
	}
	return out
}

// HasAI reports whether a template has an AI slot.
func HasAI(tpl string) bool { return len(Slots(tpl)) > 0 }

// MissingAIError names the AI slots a render had no text for.
type MissingAIError struct{ Slots []Slot }

func (e *MissingAIError) Error() string {
	keys := make([]string, len(e.Slots))
	for i, s := range e.Slots {
		keys[i] = fmt.Sprintf("%q", s.Key)
	}
	return "needs AI-written text for " + strings.Join(keys, ", ")
}

// IsMissingAI reports whether err is (or wraps) a MissingAIError.
func IsMissingAI(err error) (*MissingAIError, bool) {
	var m *MissingAIError
	ok := errors.As(err, &m)
	return m, ok
}

// NormalizeFill keys a fill the way slots are keyed, so "Summary " and
// "summary" name the same slot.
func NormalizeFill(fill map[string]string) map[string]string {
	if fill == nil {
		return nil
	}
	out := make(map[string]string, len(fill))
	for k, v := range fill {
		out[slotKey(k)] = v
	}
	return out
}

// Render fills a template: symbols from vars, AI slots from fill (by
// slot key), filters applied. A symbol without a value is an error; AI
// slots without text are collected into one *MissingAIError.
func Render(tpl string, vars, fill map[string]string) (string, error) {
	return render(tpl, vars, fill, nil)
}

func render(tpl string, vars, fill map[string]string, clean func(string) string) (string, error) {
	toks, err := parse(tpl)
	if err != nil {
		return "", err
	}
	fill = NormalizeFill(fill)
	var b strings.Builder
	var missing []Slot
	seen := map[string]bool{}
	for _, t := range toks {
		switch t.kind {
		case tokText:
			b.WriteString(t.text)
		case tokVar:
			v, ok := vars[t.text]
			if !ok {
				return "", fmt.Errorf("unknown placeholder {%s}", t.text)
			}
			v = applyFilters(v, t.filters)
			if clean != nil {
				v = clean(v)
			}
			b.WriteString(v)
		case tokAI:
			v, ok := fill[t.key]
			if !ok || strings.TrimSpace(v) == "" {
				if !seen[t.key] {
					seen[t.key] = true
					missing = append(missing, Slot{Key: t.key, Hint: t.hint})
				}
				continue
			}
			v = applyFilters(strings.TrimSpace(v), t.filters)
			if clean != nil {
				v = clean(v)
			}
			b.WriteString(v)
		}
	}
	if len(missing) > 0 {
		return "", &MissingAIError{Slots: missing}
	}
	return b.String(), nil
}

// RenderKnown fills only what vars has; every other token stays as
// written. It is the human-facing prefill (and the legacy Render).
func RenderKnown(tpl string, vars map[string]string) string {
	toks, err := parse(tpl)
	if err != nil {
		return tpl
	}
	var b strings.Builder
	for _, t := range toks {
		if t.kind == tokVar {
			if v, ok := vars[t.text]; ok {
				b.WriteString(applyFilters(v, t.filters))
				continue
			}
		}
		b.WriteString(t.raw)
	}
	return b.String()
}

// Prefill is the start of a human form: symbols filled, AI slots left
// as {ai:…} markers that FillMarkers (or the human) replaces.
func Prefill(tpl string, vars map[string]string) string {
	toks, err := parse(tpl)
	if err != nil {
		return tpl
	}
	var b strings.Builder
	for _, t := range toks {
		switch t.kind {
		case tokAI:
			b.WriteString(marker(t))
		case tokVar:
			if v, ok := vars[t.text]; ok {
				b.WriteString(applyFilters(v, t.filters))
				continue
			}
			b.WriteString(t.raw)
		default:
			b.WriteString(t.raw)
		}
	}
	return b.String()
}

func marker(t token) string {
	if legacyHints[t.key] != "" && !strings.HasPrefix(t.raw, "{ai") {
		return "{ai:" + t.key + "}"
	}
	return t.raw
}

// Markers lists the {ai…} markers left in text a human edited.
func Markers(text string) []Slot {
	toks, err := parse(text)
	if err != nil {
		return nil
	}
	var out []Slot
	seen := map[string]bool{}
	for _, t := range toks {
		if t.kind == tokAI && strings.HasPrefix(t.raw, "{ai") && !seen[t.key] {
			seen[t.key] = true
			hint := t.hint
			if h := legacyHints[t.key]; h != "" {
				hint = h
			}
			out = append(out, Slot{Key: t.key, Hint: hint})
		}
	}
	return out
}

// FillMarkers replaces the {ai…} markers in human-edited text; all other
// text (braces included) is kept as typed.
func FillMarkers(text string, fill map[string]string) (string, error) {
	toks, err := parse(text)
	if err != nil {
		return "", err
	}
	fill = NormalizeFill(fill)
	var b strings.Builder
	var missing []Slot
	for _, t := range toks {
		if t.kind == tokAI && strings.HasPrefix(t.raw, "{ai") {
			if v := strings.TrimSpace(fill[t.key]); v != "" {
				b.WriteString(applyFilters(v, t.filters))
				continue
			}
			missing = append(missing, Slot{Key: t.key, Hint: t.hint})
		}
		b.WriteString(t.raw)
	}
	if len(missing) > 0 {
		return "", &MissingAIError{Slots: missing}
	}
	return b.String(), nil
}

// Shape shows a template for one task: symbols filled where known, AI
// slots as <hint>. It is what the agent's guidance and error messages
// print.
func Shape(tpl string, vars map[string]string) string {
	toks, err := parse(tpl)
	if err != nil {
		return tpl
	}
	var b strings.Builder
	for _, t := range toks {
		switch t.kind {
		case tokText:
			b.WriteString(t.text)
		case tokVar:
			if v, ok := vars[t.text]; ok {
				b.WriteString(applyFilters(v, t.filters))
			} else {
				b.WriteString(t.raw)
			}
		case tokAI:
			h := t.hint
			if h == "" {
				h = "AI-written text"
			}
			b.WriteString("<" + h + ">")
		}
	}
	return b.String()
}

var wsRe = regexp.MustCompile(`\s+`)

// Pattern compiles a template into an anchored regexp text must match:
// symbols with a value must appear as rendered, symbols without one and
// AI slots match any text (one line unless multi). Whitespace in the
// template matches any run of whitespace.
func Pattern(tpl string, vars map[string]string, multi bool) (*regexp.Regexp, error) {
	toks, err := parse(tpl)
	if err != nil {
		return nil, err
	}
	any := `[^\n]+?`
	if multi {
		any = `(?s:.+?)`
	}
	var b strings.Builder
	b.WriteString(`^\s*`)
	for i, t := range toks {
		switch t.kind {
		case tokText:
			s := t.text
			if i == 0 {
				s = strings.TrimLeft(s, " \t\r\n")
			}
			if i == len(toks)-1 {
				s = strings.TrimRight(s, " \t\r\n")
			}
			parts := wsRe.Split(s, -1)
			for j, p := range parts {
				if j > 0 {
					b.WriteString(`\s+`)
				}
				b.WriteString(regexp.QuoteMeta(p))
			}
		case tokVar:
			if v, ok := vars[t.text]; ok {
				v = applyFilters(v, t.filters)
				parts := wsRe.Split(v, -1)
				for j, p := range parts {
					if j > 0 {
						b.WriteString(`\s+`)
					}
					b.WriteString(regexp.QuoteMeta(p))
				}
				continue
			}
			b.WriteString(any)
		case tokAI:
			b.WriteString(any)
		}
	}
	b.WriteString(`\s*$`)
	return regexp.Compile(b.String())
}

// Match checks text an author wrote against a field's template: the
// fixed parts as rendered for this task, anything in the AI slots.
func Match(field, tpl string, vars map[string]string, text string) error {
	if strings.TrimSpace(tpl) == "" {
		return nil
	}
	re, err := Pattern(tpl, vars, fieldRules[field].multi)
	if err != nil {
		return err
	}
	if !re.MatchString(text) {
		return fmt.Errorf("%s must follow %q", FieldLabel(field), Shape(tpl, vars))
	}
	return nil
}

// SlotList renders slots for an instruction: `"key": hint` per line.
func SlotList(slots []Slot) string {
	var b strings.Builder
	for _, s := range slots {
		switch h := s.Hint; {
		case h == "":
			fmt.Fprintf(&b, "  %q: text you write\n", s.Key)
		case slotKey(h) == s.Key:
			fmt.Fprintf(&b, "  %q\n", s.Key) // the key says what it is
		default:
			fmt.Fprintf(&b, "  %q: %s\n", s.Key, h)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// GenRequest asks an AI to write the slots of one piece of text: What it
// is ("commit message", "branch name", "pull request"), its Shape (fixed
// parts filled, slots as <hint>), the Slots to write, and the Context to
// write from (a diff, commits, the task).
type GenRequest struct {
	What, Shape, Context string
	Slots                []Slot
}

// Generator writes slot text, keyed by slot key. It is how DHI fills AI
// slots when no agent is in the loop: a human's commit or PR, a branch
// named before a task turn starts.
type Generator func(ctx context.Context, req GenRequest) (map[string]string, error)

// FillBranchMarkers fills the {ai…} markers left in a branch name a
// human kept (the board's prefill), each value made ref-safe and capped
// the way ExpandBranchFill does, and checks the result is a valid ref.
func FillBranchMarkers(text string, fill map[string]string) (string, error) {
	clean := map[string]string{}
	for k, v := range NormalizeFill(fill) {
		clean[k] = sanitizeRef(capRunes(v, maxBranchPart))
	}
	out, err := FillMarkers(text, clean)
	if err != nil {
		return "", err
	}
	if err := checkRef(out); err != nil {
		return "", fmt.Errorf("conventions: branch %q: %w", out, err)
	}
	return out, nil
}

// Draft is the start of a human's commit message: the subject template
// and, when set, the body format, with symbols filled and AI parts left
// as {ai:…} markers. "" when the team has neither.
func (c Commit) Draft(vars map[string]string) string {
	out := Prefill(c.Subject, vars)
	if f := strings.TrimSpace(c.BodyFormat); f != "" {
		out += "\n\n" + Prefill(f, vars)
	}
	return out
}
