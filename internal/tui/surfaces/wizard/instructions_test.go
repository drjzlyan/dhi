package wizard

import (
	"strings"
	"testing"
)

// F-090: instructions save for every workspace by default, split on ;
// and an empty form saves nothing.
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
	for _, r := range "write tests first; British English" {
		s.HandleKey(string(r))
	}
	if err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "write tests first|British English" || !everywhere {
		t.Fatalf("saved %v everywhere=%v", got, everywhere)
	}
	got = nil
	s.Enter()
	for range "write tests first; British English" {
		s.HandleKey("backspace")
	}
	if err := s.Apply(); err != nil || got != nil {
		t.Fatalf("empty form saved %v", got)
	}
}

func TestInstructionsAreACatchUpStep(t *testing.T) {
	if !catchUpIDs["instructions"] {
		t.Fatal("finished setups are never offered the new step")
	}
}
