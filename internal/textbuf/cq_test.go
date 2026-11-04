package textbuf

import "testing"

// F-165: :cq closes like :q! and marks the close as a failure, so a program
// waiting on the buffer (git) aborts; :q is not a failure.
func TestCQQuitsWithFailure(t *testing.T) {
	for _, cmd := range []string{"cq", "cq!"} {
		e := NewEditor("pick a\n")
		ex(e, cmd)
		if !e.CloseRequested() || !e.CloseForced() || !e.Aborted() {
			t.Errorf(":%s: close=%v forced=%v aborted=%v", cmd, e.CloseRequested(), e.CloseForced(), e.Aborted())
		}
	}
	e := NewEditor("pick a\n")
	ex(e, "q")
	if !e.CloseRequested() || e.Aborted() {
		t.Errorf(":q: close=%v aborted=%v", e.CloseRequested(), e.Aborted())
	}
}
