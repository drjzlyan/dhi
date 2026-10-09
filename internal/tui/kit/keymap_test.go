package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
)

func TestKeyMapTranslatesAndDisplays(t *testing.T) {
	t.Cleanup(func() { SetKeyMap(nil) })
	SetKeyMap(map[string]string{"ctrl+k": "ctrl+p", "x": "n", "same": "same"})

	if LogicalKey("ctrl+k") != "ctrl+p" || LogicalKey("x") != "n" || LogicalKey("q") != "q" {
		t.Fatal("LogicalKey translation")
	}
	cases := map[string]string{
		"ctrl+p": "ctrl+k",
		"^p":     "^k", // statusline caret form
		"n":      "x",
		"j":      "j",
		"same":   "same", // identity remaps are ignored
	}
	for in, want := range cases {
		if got := DisplayKey(in); got != want {
			t.Errorf("DisplayKey(%q) = %q, want %q", in, got, want)
		}
	}
	if got := DisplayHint("n new"); got != "x new" {
		t.Errorf("DisplayHint = %q", got)
	}
	if got := DisplayHint("s/n status"); got != "s/x status" {
		t.Errorf("alternatives: %q", got)
	}
	if got := DisplayHint("[ ] sections"); got != "[ ] sections" {
		t.Errorf("untouched pair: %q", got)
	}
	SetKeyMap(nil)
	if DisplayKey("ctrl+p") != "ctrl+p" || LogicalKey("ctrl+k") != "ctrl+k" {
		t.Fatal("SetKeyMap(nil) must clear")
	}
}

func TestHintBarShowsRemappedKeys(t *testing.T) {
	t.Cleanup(func() { SetKeyMap(nil) })
	SetKeyMap(map[string]string{"x": "n"})
	bar := HintBar(40, "", "n new", "q quit")
	if !containsPlain(bar, "x new") || containsPlain(bar, "n new") {
		t.Fatalf("hint bar = %q", bar)
	}
	if k, ok := HintKeyAt(40, "", 0, "n new"); !ok || k != "n" {
		t.Fatalf("a click on the remapped hint must still act as the logical key: %q", k)
	}
}

func containsPlain(s, sub string) bool {
	return strings.Contains(ansi.Strip(s), sub)
}

func TestKeyMapSpace(t *testing.T) {
	t.Cleanup(func() { SetKeyMap(nil) })
	SetKeyMap(map[string]string{"m": "space"}) // m marks a card
	if LogicalKey("m") != " " {
		t.Fatalf("m → %q, want the space key", LogicalKey("m"))
	}
	if DisplayHint("space mark") != "m mark" {
		t.Fatalf("hint = %q", DisplayHint("space mark"))
	}
	SetKeyMap(map[string]string{"space": "n"})
	if LogicalKey(" ") != "n" || DisplayKey("n") != "space" {
		t.Fatal("a remapped space bar")
	}
}
