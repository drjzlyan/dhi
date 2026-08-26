package reviewer

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/review"
)

// prCreateTimeout bounds the push+gh PR creation round-trip.
const prCreateTimeout = 5 * time.Minute

// modalKind enumerates overlay states.
type modalKind uint8

const (
	fNone modalKind = iota
	fNewReview
	fDiscardConfirm
	fRemoveConfirm
	fAgentReview
	fCreatePR
)

// field is one modal input: free text or a cycling toggle.
type field struct {
	label  string
	runes  []rune
	toggle []string
	val    int
}

func (f *field) text() string { return string(f.runes) }

func (f *field) cycle(dir int) {
	if len(f.toggle) == 0 {
		return
	}
	f.val = (f.val + dir + len(f.toggle)) % len(f.toggle)
}

func (f *field) toggleValue() string {
	if len(f.toggle) == 0 {
		return ""
	}
	return f.toggle[f.val]
}

// formState is the active modal (zero kind = none).
type formState struct {
	kind   modalKind
	orig   string // confirm target
	fields []field
	cur    int
	busy   bool
	err    string
	flash  string
}

func (fs *formState) target() string { return fs.orig }

func textField(label, value string) field {
	return field{label: label, runes: []rune(value)}
}

func kField() field {
	f := field{label: "kind   ", toggle: []string{"branch", "worktree", "pr"}}
	return f
}

func (m *Model) closeForm() {
	flash := m.form.flash
	m.form = formState{flash: flash}
}

func (m *Model) formKey(key string) bool {
	f := &m.form
	if f.busy {
		return true // swallow while async work runs
	}
	switch f.kind {
	case fDiscardConfirm, fRemoveConfirm:
		switch key {
		case "enter":
			m.submitConfirm()
			return true
		case "esc", "n":
			m.closeForm()
			return true
		}
		return true // confirm modals swallow everything else
	}

	switch key {
	case "esc":
		m.closeForm()
		return true
	case "enter":
		m.submitForm()
		return true
	case "tab":
		if len(f.fields) > 1 {
			f.cur = (f.cur + 1) % len(f.fields)
		}
		return true
	case "left":
		if f.fields[f.cur].toggle != nil {
			f.fields[f.cur].cycle(-1)
			return true
		}
	case "right":
		if f.fields[f.cur].toggle != nil {
			f.fields[f.cur].cycle(1)
			return true
		}
	case "backspace":
		buf := &f.fields[f.cur].runes
		if len(*buf) > 0 {
			*buf = (*buf)[:len(*buf)-1]
		}
		return true
	}
	if f.fields[f.cur].toggle == nil {
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			f.fields[f.cur].runes = append(f.fields[f.cur].runes, r[0])
			return true
		}
	}
	return false
}

func (m *Model) submitForm() {
	f := &m.form
	switch f.kind {
	case fCreatePR:
		title := strings.TrimSpace(f.fields[0].text())
		base := strings.TrimSpace(f.fields[1].text())
		id := f.orig
		if title == "" || base == "" {
			f.err = "title and base required"
			return
		}
		if m.svc == nil {
			return
		}
		m.closeForm()
		m.busy = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), prCreateTimeout)
			defer cancel()
			r, err := m.svc.CreatePR(ctx, id, title, base)
			ev := revEvent{kind: evPRCreated, id: id}
			if err != nil {
				ev.err = err.Error()
			} else {
				ev.n = r.Target.PRNumber
			}
			m.send(ev)
		}()
		return
	case fAgentReview:
		agent := strings.TrimSpace(strings.TrimPrefix(f.fields[0].text(), "@"))
		files := csvList(f.fields[1].text())
		if agent == "" {
			f.err = "agent id required"
			return
		}
		known := false
		for _, id := range m.crew.AgentIDs() {
			if id == agent {
				known = true
				break
			}
		}
		if !known {
			f.err = "unknown agent " + agent
			return
		}
		m.closeForm()
		m.requestAgentReview(agent, files)
		return
	case fNewReview:
		member := strings.TrimSpace(f.fields[0].text())
		kind := review.Kind(f.fields[1].toggleValue())
		base := strings.TrimSpace(f.fields[2].text())
		ref := strings.TrimSpace(f.fields[3].text())

		if member == "" {
			f.err = "member required"
			return
		}
		pr := 0
		switch kind {
		case review.KindPR:
			n, err := strconv.Atoi(strings.TrimPrefix(ref, "#"))
			if err != nil || n <= 0 {
				f.err = "PR number required in head/# field"
				return
			}
			pr = n
		default:
			if ref == "" {
				f.err = "head branch or sha required"
				return
			}
		}
		if !review.ValidKind(kind) {
			f.err = "bad kind"
			return
		}
		m.startReview(member, kind, base, ref, pr)
	}
}

// csvList splits a comma-separated field into trimmed entries; the "."
// sentinel passes through untouched (means "all files").
func csvList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m *Model) submitConfirm() {
	f := &m.form
	switch f.kind {
	case fDiscardConfirm:
		id := f.orig
		m.busy = true
		m.discardReview(id)
		m.closeFormWithFlash("discarding " + id + "…")
	case fRemoveConfirm:
		id := f.orig
		m.removeReview(id)
		clampCursor(&m.cursors[secReviews], len(m.reviews()))
		if m.openID == id {
			m.openID = ""
			m.files = nil
			m.diffFor = ""
		}
		m.closeForm()
	}
}

func (m *Model) closeFormWithFlash(msg string) {
	m.form = formState{flash: msg}
}
