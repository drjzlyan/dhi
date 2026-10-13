package redact

import (
	"strings"
	"testing"
)

func TestRedactsKnownValuesLongestFirst(t *testing.T) {
	vals := map[string]string{"API_TOKEN": "tok_abc123", "LONG": "tok_abc123XYZ", "PIN": "1234"}
	r := New(func() map[string]string { return vals }, nil)
	got := r.String("use tok_abc123XYZ and tok_abc123; pin 1234")
	want := "use ‹secret:LONG› and ‹secret:API_TOKEN›; pin 1234"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPicksUpNewSecretsWhenTheStampChanges(t *testing.T) {
	vals := map[string]string{}
	stamp := "1"
	r := New(func() map[string]string { return vals }, func() string { return stamp })
	if r.String("secret-value-9") != "secret-value-9" {
		t.Fatal("redacted before it was known")
	}
	vals["NEW"] = "secret-value-9"
	stamp = "2"
	if !strings.Contains(r.String("x secret-value-9 y"), "‹secret:NEW›") {
		t.Fatal("new secret not redacted after the store changed")
	}
}

func TestNilRedactsNothing(t *testing.T) {
	var r *Redactor
	if r.String("abc") != "abc" || r.Func() != nil {
		t.Fatal("nil redactor changed text")
	}
}
