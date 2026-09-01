package ideator

import (
	"strings"
)

// modalKind enumerates overlay states.
type modalKind uint8

const (
	fNone modalKind = iota
	fNewSession
	fRemoveConfirm
	fReject
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
	orig   string // confirm/reject target
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

func (m *Model) closeForm() {
	flash := m.form.flash
	m.form = formState{flash: flash}
}

func (m *Model) closeFormWithFlash(msg string) {
	m.form = formState{flash: msg}
}

func (m *Model) formKey(key string) bool {
	f := &m.form
	if f.busy {
		return true // swallow while async work runs
	}
	switch f.kind {
	case fRemoveConfirm:
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
	case fNewSession:
		name := strings.TrimSpace(f.fields[0].text())
		topic := strings.TrimSpace(f.fields[1].text())
		agents := csvList(f.fields[2].text())
		if name == "" {
			f.err = "name required"
			return
		}
		if m.store == nil {
			return
		}
		m.createSession(name, topic, agents)
		return
	case fReject:
		notes := strings.TrimSpace(f.fields[0].text())
		if notes == "" {
			f.err = "revision notes required"
			return
		}
		rel := f.orig
		if err := m.store.Reject(m.openID, rel, notes); err != nil {
			f.err = err.Error()
			return
		}
		m.closeFormWithFlash("rejected " + rel)
		m.dispatchRevision(rel, notes)
	}
}

func (m *Model) submitConfirm() {
	f := &m.form
	switch f.kind {
	case fRemoveConfirm:
		id := f.orig
		m.removeSession(id)
		if m.opErr == "" {
			m.closeFormWithFlash("removed " + id + " (artifacts kept)")
		} else {
			m.closeForm()
		}
	}
}
