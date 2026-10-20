package wizard

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/settings"
)

// F-090/F-098: instructions are a list — suggestions to tick, your own
// to add — saved for every workspace by default; nothing ticked saves
// nothing.
func TestInstructionsStepSavesForEveryWorkspace(t *testing.T) {
	var got []string
	var everywhere bool
	env := &Env{Root: "/ws",
		Instructions: func() ([]string, []string) { return nil, nil },
		SaveInstructions: func(lines []string, all bool) error {
			got, everywhere = lines, all
			return nil
		}}
	s := &instructionsStep{env: env}
	if !s.Applies() {
		t.Fatal("step hidden")
	}
	s.Enter()
	if err := s.Apply(); err != nil || got != nil {
		t.Fatalf("suggestions start unticked; saved %v", got)
	}
	s.HandleKey(" ") // tick the first suggestion
	s.HandleKey("a")
	if !s.CapturesText() {
		t.Fatal("typing a rule must capture text")
	}
	for _, r := range "British English" {
		s.HandleKey(string(r))
	}
	s.HandleKey("enter")
	if s.CapturesText() {
		t.Fatal("enter adds the rule and leaves typing")
	}
	if err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != suggestedRules[0]+"|British English" || !everywhere {
		t.Fatalf("saved %v everywhere=%v", got, everywhere)
	}
	// Your own rule can be deleted; a suggestion cannot.
	s.HandleKey("d")
	s.HandleKey("s") // this workspace
	got = nil
	if err := s.Apply(); err != nil || strings.Join(got, "|") != suggestedRules[0] || everywhere {
		t.Fatalf("after delete + scope: saved %v everywhere=%v", got, everywhere)
	}
}

func TestInstructionsKeepExistingRulesTicked(t *testing.T) {
	env := &Env{Root: "/ws",
		Instructions:     func() ([]string, []string) { return nil, []string{"say the test command"} },
		SaveInstructions: func([]string, bool) error { return nil }}
	s := &instructionsStep{env: env}
	s.Enter()
	if l := s.lines(); len(l) != 1 || l[0] != "say the test command" || s.scope != 1 {
		t.Fatalf("lines %v scope %d", l, s.scope)
	}
}

func TestInstructionsAreACatchUpStep(t *testing.T) {
	if !catchUpIDs["instructions"] {
		t.Fatal("finished setups are never offered the new step")
	}
}

// F-090 (live): a second workspace does not ask for what you already set
// for every workspace.
func TestYoursAreNotAskedAgain(t *testing.T) {
	env := &Env{Root: "/ws2",
		Instructions:     func() ([]string, []string) { return []string{"say the test command"}, nil },
		SaveInstructions: func([]string, bool) error { return nil },
		SaveProfile:      func(settings.Profile) error { return nil }, ProfileSaved: true}
	if (&instructionsStep{env: env}).Applies() || (&styleStep{env: env}).Applies() {
		t.Fatal("asked again for what is already yours everywhere")
	}
}
