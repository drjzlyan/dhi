package bus

import (
	"strings"
	"testing"
)

// F-088: a posted text never carries a known secret value — not in the
// delivered message, not in the channel file.
func TestPostRedactsSecrets(t *testing.T) {
	b, err := Open(fixtureWS(t))
	if err != nil {
		t.Fatal(err)
	}
	b.Redact = func(s string) string { return strings.ReplaceAll(s, "hunter2-token", "‹secret:T›") }
	m, err := b.Post(Message{Channel: "#general", Author: "anvil", Text: "token is hunter2-token"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Text, "hunter2") {
		t.Fatalf("posted %q", m.Text)
	}
	for _, h := range b.History("#general", 0) {
		if strings.Contains(h.Text, "hunter2") {
			t.Fatalf("persisted %q", h.Text)
		}
	}
}
