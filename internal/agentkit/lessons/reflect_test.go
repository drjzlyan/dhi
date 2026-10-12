package lessons

import (
	"strings"
	"testing"
)

func TestReflectionPromptCarriesEvidence(t *testing.T) {
	p := ReflectionPrompt(Evidence{
		Goal: "fix Split", Outcome: "fixed", Signals: []Signal{SignalVerify, SignalCorrection},
		Failures: []string{"--- FAIL: TestSplit want 334 got 333"}, Correction: "don't change the tests",
		Known: []string{"Run go test with -race"},
	})
	for _, want := range []string{"Goal: fix Split", "verify, correction", "TestSplit", "don't change the tests", "do not repeat these", `"lessons"`, "Do not use any tools"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestParseReflection(t *testing.T) {
	got, err := ParseReflection("Sure!\n```json\n{\"lessons\":[{\"text\":\"Distribute remainders one cent at a time\",\"tags\":[\"money\"],\"scope\":\"private\",\"confidence\":0.8},{\"text\":\"Prefer terse commit subjects\",\"scope\":\"user\",\"confidence\":7},{\"text\":\"\"}]}\n```")
	if err != nil || len(got) != 2 {
		t.Fatalf("parse = %+v %v", got, err)
	}
	if got[0].Scope != ScopePrivate || got[0].Confidence != 0.8 || got[1].Scope != ScopeUser || got[1].Confidence != 0.6 || got[1].Source != SourceReflection {
		t.Fatalf("parsed = %+v", got)
	}
	if got, err := ParseReflection(`{"lessons":[]}`); err != nil || len(got) != 0 {
		t.Fatalf("empty = %+v %v", got, err)
	}
	if _, err := ParseReflection("I learned nothing"); err == nil {
		t.Fatal("no JSON must be an error")
	}
}

func TestIsCorrection(t *testing.T) {
	for text, want := range map[string]bool{
		"No, use the existing helper":         true,
		"don't touch the tests":               true,
		"Use errors.Is instead":               true,
		"why did you delete that file?":       true,
		"great, now add a Delete method":      false,
		"thanks!":                             false,
		"can you also handle the empty case?": false,
	} {
		if got := IsCorrection(text); got != want {
			t.Errorf("IsCorrection(%q) = %v, want %v", text, got, want)
		}
	}
}
