package conventions

import (
	"strings"
	"testing"
)

func TestBuiltinsValidateAndActivate(t *testing.T) {
	for _, b := range Builtins() {
		c := Defaults()
		if err := c.Activate(b.Name); err != nil {
			t.Fatal(err)
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("builtin %s invalid: %v", b.Name, err)
		}
		if c.Active != b.Name || c.Branch.Task != b.Branch.Task {
			t.Fatalf("%s not applied: %+v", b.Name, c)
		}
	}
	c := Defaults()
	if err := c.Activate("nope"); err == nil {
		t.Fatal("unknown template activated")
	}
}

func TestUserTemplateShadowsAndValidates(t *testing.T) {
	c := Defaults()
	user := c.Snapshot()
	user.Branch.Task = "dev/{user}/{slug}"
	c.Templates = map[string]Template{"team": user, "feature": user}
	names := strings.Join(c.TemplateNames(), ",")
	if names != "default,ticket,personal,feature,team" {
		t.Fatalf("names = %s", names)
	}
	if _, builtin, _ := c.Lookup("feature"); builtin {
		t.Fatal("user template must shadow the built-in")
	}
	if err := c.Activate("team"); err != nil || c.Branch.Task != "dev/{user}/{slug}" {
		t.Fatalf("activate team: %v %+v", err, c.Branch)
	}
	bad := user
	bad.Worktree.Task = "{slug}"
	c.Templates["broken"] = bad
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "templates.broken") {
		t.Fatalf("broken template accepted: %v", err)
	}
}

func TestExpandWorktree(t *testing.T) {
	v := map[string]string{"slug": "login", "member": "api", "ticket": "PROJ-1", "user": "u", "date": "d"}
	for pat, want := range map[string]string{
		"{slug}/{member}":        "login/api",
		"{ticket}/{member}":      "PROJ-1/api",
		"{member}-{slug}":        "api-login",
		"{user}/{date}/{member}": "u/d/api",
	} {
		got, err := ExpandWorktree(pat, v)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", pat, got, err, want)
		}
	}
	v["slug"] = "../../etc" // values are made path-safe, never navigation
	if got, err := ExpandWorktree("{slug}/{member}", v); err != nil || strings.Contains(got, "..") {
		t.Fatalf("unsafe slug gave %q, %v", got, err)
	}
	for _, bad := range []string{"/{member}", "{slug}", "../{member}", "{slug}//{member}"} {
		if _, err := ExpandWorktree(bad, v); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTicketForAndPreview(t *testing.T) {
	if got := TicketFor(`[A-Z][A-Z0-9]+-\d+`, "proj-42-login", "Fix"); got != "PROJ-42" {
		t.Fatalf("ticket = %q", got)
	}
	if got := TicketFor(`[A-Z][A-Z0-9]+-\d+`, "login"); got != "" {
		t.Fatalf("ticket = %q", got)
	}
	c := Defaults()
	_ = c.Activate("ticket")
	p := strings.Join(c.Preview(), "\n")
	for _, want := range []string{"branch    PROJ-123/login-timeout", "worktree  .dhi/tasks/PROJ-123/api",
		"commit    PROJ-123: retry the token refresh", "PR title  [PROJ-123] Fix login timeout"} {
		if !strings.Contains(p, want) {
			t.Fatalf("preview missing %q:\n%s", want, p)
		}
	}
	if !strings.Contains(c.Guidance(), `"{ticket}: {subject}"`) {
		t.Fatalf("guidance lacks the subject shape:\n%s", c.Guidance())
	}
}
