package forge

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func recipe() Tool {
	return Tool{
		Name: "race_test", Description: "Run the Go tests of one package with the race detector", Scope: "exec",
		Inputs:     []Input{{Name: "pkg", Type: "string", Description: "package path", Required: true}, {Name: "count", Type: "integer"}},
		Steps:      []Step{{Run: []string{"go", "test", "-race", "-count={{count}}", "{{pkg}}"}}},
		ProposedBy: "dev", Rationale: "I ran this in 4 of my last 6 runs",
	}
}

func allowGo(argv []string) error {
	if argv[0] != "go" {
		return fmt.Errorf("program %q not allowed", argv[0])
	}
	return nil
}

func TestValidate(t *testing.T) {
	if err := recipe().Validate(allowGo); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Tool){
		"name":              func(x *Tool) { x.Name = "Bad-Name" },
		"description":       func(x *Tool) { x.Description = " " },
		"scope":             func(x *Tool) { x.Scope = "push" },
		"both bodies":       func(x *Tool) { x.Script = "#!/bin/sh\necho" },
		"no body":           func(x *Tool) { x.Steps = nil },
		"undeclared":        func(x *Tool) { x.Steps[0].Run = append(x.Steps[0].Run, "{{nope}}") },
		"templated program": func(x *Tool) { x.Steps[0].Run[0] = "{{pkg}}" },
		"not allowlisted":   func(x *Tool) { x.Steps[0].Run[0] = "curl" },
		"input type":        func(x *Tool) { x.Inputs[1].Type = "float" },
	}
	for name, mut := range bad {
		tl := recipe()
		tl.Steps = []Step{{Run: append([]string(nil), tl.Steps[0].Run...)}}
		tl.Inputs = append([]Input(nil), tl.Inputs...)
		mut(&tl)
		if err := tl.Validate(allowGo); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	script := Tool{Name: "lint_all", Description: "Lint everything", Scope: "exec", Script: "#!/bin/sh\ngo vet ./...\n"}
	if err := script.Validate(nil); err != nil {
		t.Fatal(err)
	}
	script.Scope = "read"
	if err := script.Validate(nil); err == nil {
		t.Fatal("a script tool must be exec scope")
	}
}

func TestArgsAndExpand(t *testing.T) {
	tl := recipe()
	args, err := tl.Args(map[string]any{"pkg": "./money", "count": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	got := tl.Expand(args)
	if strings.Join(got[0], " ") != "go test -race -count=1 ./money" {
		t.Fatalf("expand = %v", got)
	}
	// An optional input left out drops its argument.
	args, _ = tl.Args(map[string]any{"pkg": "./x; rm -rf /"})
	if got := tl.Expand(args); strings.Join(got[0], "|") != "go|test|-race|./x; rm -rf /" {
		t.Fatalf("templating must never split or shell-evaluate: %q", got[0])
	}
	for _, raw := range []map[string]any{{}, {"pkg": 3}, {"pkg": "a\nb"}, {"pkg": "x", "count": 1.5}, {"pkg": "x", "extra": 1}} {
		if _, err := tl.Args(raw); err == nil {
			t.Errorf("args %v: want a refusal", raw)
		}
	}
	if s := tl.InputSchema(); !strings.Contains(s, `"pkg":{"type":"string"`) || !strings.Contains(s, `"required":["pkg"]`) {
		t.Fatalf("schema = %s", s)
	}
}

func TestStoreLifecycle(t *testing.T) {
	s := Open(t.TempDir())
	s.now = func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) }
	p, err := s.Propose(recipe(), allowGo)
	if err != nil || p.Version != 1 {
		t.Fatalf("propose = %+v %v", p, err)
	}
	if act, _ := s.Active(); len(act) != 0 {
		t.Fatal("a proposal must not be active")
	}
	if pend, _ := s.Pending(); len(pend) != 1 {
		t.Fatal("proposal not pending")
	}
	a, err := s.Approve("race_test", "you")
	if err != nil || a.ApprovedBy != "you" || a.Approved.IsZero() {
		t.Fatalf("approve = %+v %v", a, err)
	}
	if act, _ := s.Active(); len(act) != 1 {
		t.Fatal("approved tool not active")
	}
	// An edit is version 2 and needs approval again; v1 stays active.
	v2, _ := s.Propose(recipe(), allowGo)
	if v2.Version != 2 {
		t.Fatalf("edit version = %d", v2.Version)
	}
	if err := s.Reject("race_test"); err != nil {
		t.Fatal(err)
	}
	s.RecordUse("race_test")
	s.RecordUse("race_test")
	if u := s.Usages()["race_test"]; u.Calls != 2 {
		t.Fatalf("usage = %+v", u)
	}
	if err := s.Archive("race_test"); err != nil {
		t.Fatal(err)
	}
	if act, _ := s.Active(); len(act) != 0 {
		t.Fatal("archived tool still active")
	}
	// A script tool writes its executable on approval.
	sc := Tool{Name: "lint_all", Description: "Lint", Scope: "exec", Script: "#!/bin/sh\necho ok\n"}
	if _, err := s.Propose(sc, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve("lint_all", "you"); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(s.ScriptPath("lint_all")); err != nil || st.Mode()&0o100 == 0 {
		t.Fatalf("script not executable: %v", err)
	}
}

func TestDetect(t *testing.T) {
	runs := [][]string{
		{"bash: go vet ./...", "bash: go test -race ./...", "bash: ls"},
		{"bash: cd /w/app && go vet ./...", "bash:  go test   -race ./...", "command: git diff"},
		{"bash: go vet ./...", "bash: go test -race ./..."},
		{"bash: go build ./..."},
	}
	got := Detect(runs, 3)
	if len(got) != 1 || strings.Join(got[0].Commands, " ; ") != "go vet ./... ; go test -race ./..." || got[0].Runs != 3 {
		t.Fatalf("detect = %+v", got)
	}
	if len(Detect(runs, 4)) != 0 {
		t.Fatal("nothing recurs in 4 runs")
	}
}
