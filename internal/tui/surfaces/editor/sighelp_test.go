package editor

import (
	"strings"
	"testing"
)

func TestSignatureHelpFollowsTheCall(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "i")
	typeKeys(m, "Add(")
	waitFor(t, m, func() bool { return m.sigHelp != nil }, "signature popup")
	if v := plainView(m); !strings.Contains(v, "func Add(a int, b int) int") || !strings.Contains(v, "Add sums two ints.") {
		t.Errorf("popup missing signature or doc:\n%s", v)
	}
	if h := m.sigHelp; h.ParamStart != 9 || h.ParamEnd != 14 {
		t.Errorf("active param = [%d,%d), want [9,14) (a int)", h.ParamStart, h.ParamEnd)
	}

	typeKeys(m, "1,") // "," moves the active parameter
	waitFor(t, m, func() bool { return m.sigHelp != nil && m.sigHelp.ParamStart == 16 }, "second parameter")
	if h := m.sigHelp; h.ParamEnd != 21 {
		t.Errorf("active param end = %d, want 21 (b int, label offsets)", h.ParamEnd)
	}

	typeKeys(m, "2)") // closing the call dismisses it
	if m.sigHelp != nil || strings.Contains(plainView(m), "Add sums two ints.") {
		t.Error("popup should close on )")
	}
}

func TestSignatureHelpEscDismisses(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "i")
	typeKeys(m, "Add(")
	waitFor(t, m, func() bool { return m.sigHelp != nil }, "signature popup")
	feed(m, "esc")
	if m.sigHelp != nil || strings.Contains(plainView(m), "Add sums two ints.") {
		t.Error("popup should close on esc")
	}
}

func TestSignatureHelpLeavingTheCallDismisses(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "i")
	typeKeys(m, "Add(")
	waitFor(t, m, func() bool { return m.sigHelp != nil }, "signature popup")
	feed(m, "backspace") // deletes the "(" (and its auto-pair): no call any more
	if m.sigHelp != nil {
		t.Error("popup should close once the cursor is outside the call")
	}
}

func TestSignatureHelpStaleAnswerIsDropped(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "i")
	typeKeys(m, "Add(")
	feed(m, "esc") // leaves insert before the answer is applied
	m.drainTerm()  // the late answer arrives now
	if m.sigHelp != nil {
		t.Error("an answer that lands after esc must not reopen the popup")
	}
}

func TestSignatureHelpNeedsInsertMode(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	feed(m, "(") // normal mode: not a trigger
	m.drainTerm()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, ev := range srv.events {
		if ev == "signatureHelp" {
			t.Error("signature help requested in normal mode")
		}
	}
}

func TestOpenParensBefore(t *testing.T) {
	for _, c := range []struct {
		line string
		col  int
		want int
	}{
		{"f(", 2, 1}, {"f(a, ", 5, 1}, {"f(g(x)", 6, 1}, {"f(g(x))", 7, 0}, {"f(a)", 1, 0}, {"f(", 99, 1}, {"a)", 2, 0},
	} {
		if got := openParensBefore(c.line, c.col); got != c.want {
			t.Errorf("openParensBefore(%q,%d) = %d, want %d", c.line, c.col, got, c.want)
		}
	}
}
