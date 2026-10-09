package reviewer

import "testing"

// TestFailedStartLeavesFormUsable: a start that fails (no network, a
// bad PR number) must show the error in the dialog and let the user edit
// and retry; esc closes a dialog even while work runs. It used to stay
// on "working…" with every key swallowed.
func TestFailedStartLeavesFormUsable(t *testing.T) {
	m := New("0.1.0", nil, Deps{})
	m.form = formState{kind: fNewReview, busy: true, fields: []field{textField("head/# ", "6")}}
	m.Update(revEvent{kind: evStartDone, err: "fetch pull/6: offline"})
	if m.form.busy || m.form.err == "" || m.form.kind != fNewReview {
		t.Fatalf("form after a failed start = busy:%v err:%q kind:%v", m.form.busy, m.form.err, m.form.kind)
	}
	m.formKey("7") // editable again
	if got := m.form.fields[0].text(); got != "67" {
		t.Fatalf("field = %q, want the form to take input again", got)
	}

	m.form.busy = true
	m.formKey("esc")
	if m.form.kind != fNone {
		t.Fatal("esc must close a dialog while work runs")
	}
}
