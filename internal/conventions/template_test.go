package conventions

import (
	"strings"
	"testing"
)

var tv = map[string]string{
	"ticket": "PROJ-7", "slug": "login-timeout", "id": "login-timeout", "title": "Fix Login Timeout",
	"member": "api", "branch": "task/login-timeout", "assignee": "forge", "user": "dana", "date": "20261105",
}

func TestRenderSymbolsFiltersAndAI(t *testing.T) {
	got, err := Render("{ticket|lower}/{title|kebab} {title|snake} {user|upper}: {ai:summary} — {ai:summary} {{ok}}",
		tv, map[string]string{"Summary ": "Retry it"})
	want := "proj-7/fix-login-timeout fix_login_timeout DANA: Retry it — Retry it {ok}"
	if err != nil || got != want {
		t.Fatalf("got %q, %v\nwant %q", got, err, want)
	}
}

func TestRenderNamesEveryMissingSlotOnce(t *testing.T) {
	_, err := Render("{ai:why} {ai} {ai:why} {subject}", tv, nil)
	m, ok := IsMissingAI(err)
	if !ok || len(m.Slots) != 3 || m.Slots[0].Key != "why" || m.Slots[1].Key != "ai" || m.Slots[2].Key != "subject" {
		t.Fatalf("err = %v", err)
	}
	if _, err := Render("{nope}", tv, nil); err == nil || !strings.Contains(err.Error(), "{nope}") {
		t.Fatalf("missing symbol: %v", err)
	}
}

// Brace text that is not a token stays literal: prose and code in a PR
// body need no escaping.
func TestLiteralBracesStay(t *testing.T) {
	got, err := Render("func() { return {} } {Not A Token} {a b}", nil, nil)
	if err != nil || got != "func() { return {} } {Not A Token} {a b}" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestValidateFieldRules(t *testing.T) {
	ok := map[string]string{
		FieldBranchTask:    "{user}/{member}/{ticket}-{ai:short name|kebab}",
		FieldBranchReview:  "review/{id}",
		FieldWorktree:      "{ticket}/{member}",
		FieldCommitSubject: "{type}({member}): {subject}",
		FieldCommitBody:    "Why: {ai:why}\n\nRefs {ticket}",
		FieldPRTitle:       "[{ticket}] {title}",
		FieldPRBody:        "## Summary\n{ai:summary}\n\nBranch `{branch}` by {assignee}",
	}
	for f, tpl := range ok {
		if err := ValidateField(f, tpl); err != nil {
			t.Errorf("%s %q: %v", f, tpl, err)
		}
	}
	bad := []struct{ field, tpl, want string }{
		{FieldBranchTask, "{subject}", "unknown placeholder {subject}"},
		{FieldBranchReview, "review/{ai:name}", "{ai} is not allowed"},
		{FieldWorktree, "{ai}/{member}", "{ai} is not allowed"},
		{FieldPRTitle, "{type}: {title}", "unknown placeholder {type}"},
		{FieldPRBody, "{title|shout}", "unknown filter |shout"},
	}
	for _, c := range bad {
		if err := ValidateField(c.field, c.tpl); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: err = %v, want %q", c.field, c.tpl, err, c.want)
		}
	}
}

func TestMatchFixedPartsAndSlots(t *testing.T) {
	tpl := "{ticket}: {ai:summary}"
	if err := Match(FieldCommitSubject, tpl, tv, "PROJ-7: retry the refresh"); err != nil {
		t.Fatal(err)
	}
	err := Match(FieldCommitSubject, tpl, tv, "PROJ-8: retry the refresh")
	if err == nil || !strings.Contains(err.Error(), `"PROJ-7: <summary>"`) {
		t.Fatalf("wrong ticket: %v", err)
	}
	// Unknown symbols are wildcards; whitespace runs are lenient; regexp
	// metacharacters in fixed text are literal.
	if err := Match(FieldCommitSubject, "[{ticket}] (x) {ai}", nil, "[ANY-1]  (x) done"); err != nil {
		t.Fatal(err)
	}
	body := "## Summary\n{ai:summary}\n\n## Testing\n{ai:testing}"
	if err := Match(FieldPRBody, body, tv, "## Summary\nline one\nline two\n\n## Testing\nran go test\n"); err != nil {
		t.Fatal(err)
	}
	if err := Match(FieldPRBody, body, tv, "## Summary\nx"); err == nil {
		t.Fatal("a missing section must not match")
	}
}

func TestPrefillMarkersAndFill(t *testing.T) {
	text := Prefill("{ticket}: {subject} {ai:why|lower}", tv)
	if text != "PROJ-7: {ai:subject} {ai:why|lower}" {
		t.Fatalf("prefill = %q", text)
	}
	ms := Markers(text)
	if len(ms) != 2 || ms[0].Key != "subject" || ms[0].Hint != legacyHints["subject"] || ms[1].Key != "why" {
		t.Fatalf("markers = %+v", ms)
	}
	// The human's own braces survive; only markers are filled.
	got, err := FillMarkers("{x} "+text, map[string]string{"subject": "retry it", "why": "FLAKY"})
	if err != nil || got != "{x} PROJ-7: retry it flaky" {
		t.Fatalf("fill = %q, %v", got, err)
	}
}

func TestShapeAndSlots(t *testing.T) {
	if got := Shape("{ticket}: {ai}", tv); got != "PROJ-7: <AI-written text>" {
		t.Fatalf("shape = %q", got)
	}
	sl := Slots("{type}: {subject} {ai:why} {ai: Why }")
	if len(sl) != 3 || sl[2].Key != "why" {
		t.Fatalf("slots = %+v", sl)
	}
}

func TestExpandBranchFillMakesAIPartsRefSafe(t *testing.T) {
	got, err := ExpandBranchFill("{user}/{ai:name}", tv, map[string]string{"name": "Retry the token refresh on 401 / timeouts and also many more words here"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "dana/Retry-the-token-refresh-on-401-timeouts-and-al" && !strings.HasPrefix(got, "dana/Retry-the-token-refresh") {
		t.Fatalf("branch = %q", got)
	}
	if len(got) > len("dana/")+maxBranchPart {
		t.Fatalf("branch part not capped: %q", got)
	}
	if _, err := ExpandBranchFill("{user}/{ai:name}", tv, nil); err == nil {
		t.Fatal("a missing AI part must be an error")
	} else if _, ok := IsMissingAI(err); !ok {
		t.Fatalf("err = %v, want MissingAIError", err)
	}
}

func TestCheckCommitForEnforcesSubjectAndBodyFormat(t *testing.T) {
	c := Defaults().Commit
	c.Subject, c.BodyFormat = "{ticket}: {subject}", "Why: {ai:why}\n\nRefs {ticket}"
	good := "PROJ-7: retry the refresh\n\nWhy: tokens expire\nmid-request\n\nRefs PROJ-7\n\nCo-Authored-By: A <a@b.c>"
	if err := c.CheckCommitFor(good, tv); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckCommitFor("retry the refresh", tv); err == nil || !strings.Contains(err.Error(), "PROJ-7: <imperative summary") {
		t.Fatalf("subject drift: %v", err)
	}
	if err := c.CheckCommitFor("PROJ-7: retry\n\nsomething else", tv); err == nil || !strings.Contains(err.Error(), "commit body must follow") {
		t.Fatalf("body drift: %v", err)
	}
	msg, err := c.ComposeCommit(tv, map[string]string{"subject": "retry the refresh", "why": "tokens expire"}, "")
	if err != nil || msg != "PROJ-7: retry the refresh\n\nWhy: tokens expire\n\nRefs PROJ-7" {
		t.Fatalf("compose = %q, %v", msg, err)
	}
	if err := c.CheckCommitFor(msg, tv); err != nil {
		t.Fatalf("a composed message must pass its own check: %v", err)
	}
}

func TestGuidanceForShowsShapesAndSlots(t *testing.T) {
	c := Defaults()
	c.Branch.Task = "{user}/{ai:short name|kebab}"
	c.Commit.Subject = "{ticket}: {subject}"
	c.PR.Body = "## Summary\n{ai:summary}"
	g := c.GuidanceFor(tv)
	for _, want := range []string{
		`tasks use "dana/<short name>"`,
		`shape "PROJ-7: <imperative summary of the change>"`,
		"    ## Summary\n    <summary>",
		"  \"short name\"\n", `"subject": imperative summary`, "  \"summary\"",
		"Commit only with git_commit",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guidance lacks %q:\n%s", want, g)
		}
	}
}

// Every symbol a field allows has a sample value, so Settings validation
// never passes a template that cannot render (the SampleVars bug).
func TestSampleVarsCoverEverySymbol(t *testing.T) {
	sv := SampleVars()
	for _, s := range Symbols {
		if _, ok := sv[s.Name]; !ok {
			t.Errorf("no sample for {%s}", s.Name)
		}
	}
}

// A ticket-format team whose card has no ticket key: DHI fills {ticket}
// with the slug, and a message composed from the template must still
// pass its own check (the coarse format check gives way to the shape).
func TestTicketFormatWithASlugFallbackStillCommits(t *testing.T) {
	c := Defaults()
	if err := c.Activate("ticket"); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"ticket": "fix-login", "slug": "fix-login"}
	msg, err := c.Commit.ComposeCommit(vars, map[string]string{"subject": "retry the refresh"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Commit.CheckCommitFor(msg, vars); err != nil {
		t.Fatalf("composed %q refused: %v", msg, err)
	}
	// With a real key the format check still applies to a hand-written message.
	if err := c.Commit.CheckCommitFor("no key here", map[string]string{"ticket": "PROJ-1"}); err == nil {
		t.Fatal("a subject without the key must be refused")
	}
	if g := c.GuidanceFor(vars); strings.Contains(g, "start with a ticket key") {
		t.Fatalf("guidance must not ask for a key the card does not have:\n%s", g)
	}
}
