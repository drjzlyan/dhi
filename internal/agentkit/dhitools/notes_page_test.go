package dhitools

import (
	"strings"
	"testing"
)

func TestNotesPage(t *testing.T) {
	notes := strings.Repeat("a", 10) + strings.Repeat("b", 10)
	if got := notesPage(notes, 0, 10); !strings.HasPrefix(got, strings.Repeat("a", 10)) || !strings.Contains(got, "offset=10") {
		t.Fatalf("page 1 = %q", got)
	}
	if got := notesPage(notes, 10, 10); got != strings.Repeat("b", 10) {
		t.Fatalf("page 2 = %q", got)
	}
	if got := notesPage(notes, 50, 10); !strings.Contains(got, "end of notes") {
		t.Fatalf("past end = %q", got)
	}
}
