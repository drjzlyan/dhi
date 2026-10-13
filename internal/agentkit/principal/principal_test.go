package principal

import (
	"strings"
	"testing"
)

func TestVoiceDefaults(t *testing.T) {
	v := Profile{}.Voice()
	for _, want := range []string{
		"You work for the user (developer)", "ships under their git identity",
		"no tool logs", "fenced ``` blocks", "outcome in the first sentence", "short plain paragraphs",
		"a plain tone", "no emoji", "in English",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("voice lacks %q:\n%s", want, v)
		}
	}
}

func TestVoiceFollowsTheProfile(t *testing.T) {
	v := Profile{Name: "Ada", Role: "founder", Tone: "casual", Length: "detailed", Emoji: true, Language: "German",
		Order: "steps-first", Format: "bullets", Detail: "brief"}.Voice()
	for _, want := range []string{"You work for Ada (founder)", "Ada reads replies best walking through",
		"short bullet points", "few lines", "in Ada's name", "casual tone", "trade-offs", "emoji welcome", "in German"} {
		if !strings.Contains(v, want) {
			t.Errorf("voice lacks %q:\n%s", want, v)
		}
	}
}

func TestPersonaFollowsDetail(t *testing.T) {
	for detail, want := range map[string]string{"": "", "normal": "", "brief": "terse", "detailed": "meticulous"} {
		if got := (Profile{Detail: detail}).Persona(); got != want {
			t.Errorf("%q → %q, want %q", detail, got, want)
		}
	}
}
