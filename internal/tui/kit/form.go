package kit

import (
	"strings"
	"unicode/utf8"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Field is one form input: a text buffer or a cycling toggle choice.
// Text fields carry an in-value cursor: left/right move it, runes and
// paste insert at it, backspace/delete edit around it (F-026 P1d).
type Field struct {
	Label  string
	Value  string   // text fields
	Toggle []string // non-empty: left/right cycles choices
	sel    int      // selected toggle index
	cur    int      // text-field cursor (rune index)
}

// NewTextField builds a free-text field pre-filled with value; the
// cursor starts at the end of the pre-fill.
func NewTextField(label, value string) Field {
	return Field{Label: label, Value: value, cur: utf8.RuneCountInString(value)}
}

// NewToggleField builds a cycling choice field; initial selects the
// starting choice (clamped).
func NewToggleField(label string, choices []string, initial int) Field {
	f := Field{Label: label, Toggle: choices}
	if len(choices) > 0 {
		if initial < 0 {
			initial = 0
		}
		if initial >= len(choices) {
			initial = len(choices) - 1
		}
		f.sel = initial
	}
	return f
}

// FormAction is what HandleKey decided.
type FormAction int

const (
	FormNone FormAction = iota
	FormSubmit
	FormCancel
)

// Form is the canonical dialog form (F-024): labeled fields, tab /
// shift+tab to cycle, left/right cycle toggles or move the text
// cursor, enter submits, esc cancels, busy swallows input. Submit
// handling stays with the surface — call Values/ToggleIndex inside
// your submit branch.
type Form struct {
	Title  string
	Fields []Field
	cur    int
	Err    string
	Busy   bool
}

// NewForm builds a form. Preserve field order: Values() indexes match.
func NewForm(title string, fields ...Field) *Form {
	return &Form{Title: title, Fields: fields}
}

// SetError sets the error line shown under the body.
func (f *Form) SetError(s string) { f.Err = s }

// Values returns the current value of every field (toggle fields
// report their selected choice).
func (f *Form) Values() []string {
	out := make([]string, len(f.Fields))
	for i, fl := range f.Fields {
		if len(fl.Toggle) > 0 {
			out[i] = fl.Toggle[fl.sel]
		} else {
			out[i] = fl.Value
		}
	}
	return out
}

// ToggleIndex returns the selected choice index of field i.
func (f *Form) ToggleIndex(i int) int {
	if i < 0 || i >= len(f.Fields) {
		return 0
	}
	return f.Fields[i].sel
}

// Cur returns the active field index (tests and callers that need to
// know where typing will land).
func (f *Form) Cur() int { return f.cur }

// HandleKey implements the canonical contract; busy swallows all.
// Unknown keys are consumed anyway while the form is open (the
// focus-trap rule) — the surface decides pre-form keys.
func (f *Form) HandleKey(key string) FormAction {
	if f.Busy {
		return FormNone
	}
	switch key {
	case "esc":
		return FormCancel
	case "enter":
		return FormSubmit
	case "tab":
		if len(f.Fields) > 1 {
			f.cur = (f.cur + 1) % len(f.Fields)
		}
		return FormNone
	case "shift+tab":
		if len(f.Fields) > 1 {
			f.cur = (f.cur - 1 + len(f.Fields)) % len(f.Fields)
		}
		return FormNone
	case "backspace":
		fl := &f.Fields[f.cur]
		if len(fl.Toggle) == 0 && fl.cur > 0 {
			fl.Value = f.deleteRuneAt(fl.Value, fl.cur-1)
			fl.cur--
		}
		return FormNone
	case "delete", "ctrl+d":
		fl := &f.Fields[f.cur]
		if len(fl.Toggle) == 0 && fl.cur < utf8.RuneCountInString(fl.Value) {
			fl.Value = f.deleteRuneAt(fl.Value, fl.cur)
		}
		return FormNone
	case "home", "ctrl+a":
		if len(f.Fields[f.cur].Toggle) == 0 {
			f.Fields[f.cur].cur = 0
		}
		return FormNone
	case "end", "ctrl+e":
		fl := &f.Fields[f.cur]
		if len(fl.Toggle) == 0 {
			fl.cur = utf8.RuneCountInString(fl.Value)
		}
		return FormNone
	case "left":
		if len(f.Fields[f.cur].Toggle) > 0 {
			f.cycleCur(-1)
			return FormNone
		}
		if f.Fields[f.cur].cur > 0 {
			f.Fields[f.cur].cur--
		}
		return FormNone
	case "right":
		if len(f.Fields[f.cur].Toggle) > 0 {
			f.cycleCur(1)
			return FormNone
		}
		if f.Fields[f.cur].cur < utf8.RuneCountInString(f.Fields[f.cur].Value) {
			f.Fields[f.cur].cur++
		}
		return FormNone
	}
	fl := &f.Fields[f.cur]
	if len(fl.Toggle) == 0 {
		if text, ok := strings.CutPrefix(key, "paste:"); ok {
			f.insertRunes(fl, []rune(text))
			return FormNone
		}
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			f.insertRunes(fl, r)
		}
	}
	return FormNone
}

// insertRunes inserts rs at the field's cursor and advances it.
func (f *Form) insertRunes(fl *Field, rs []rune) {
	rs = f.filterPrintable(rs)
	if len(rs) == 0 {
		return
	}
	rvalue := []rune(fl.Value)
	out := make([]rune, 0, len(rvalue)+len(rs))
	out = append(out, rvalue[:fl.cur]...)
	out = append(out, rs...)
	out = append(out, rvalue[fl.cur:]...)
	fl.Value = string(out)
	fl.cur += len(rs)
}

func (f *Form) filterPrintable(rs []rune) []rune {
	out := rs[:0]
	for _, r := range rs {
		if r >= 32 {
			out = append(out, r)
		}
	}
	return out
}

// deleteRuneAt removes the rune at rune index i from s.
func (f *Form) deleteRuneAt(s string, i int) string {
	rs := []rune(s)
	if i < 0 || i >= len(rs) {
		return s
	}
	return string(append(rs[:i:i], rs[i+1:]...))
}

func (f *Form) cycleCur(dir int) {
	fl := &f.Fields[f.cur]
	if len(fl.Toggle) == 0 {
		return
	}
	fl.sel = (fl.sel + dir + len(fl.Toggle)) % len(fl.Toggle)
}

// View renders the form body lines (without the modal box): label,
// value rows with the active-field cursor, then the hint line.
// Focus is visible: only the active field renders on the accent
// style; inactive fields are dim (F-026 P1d).
func (f *Form) View() []string {
	hint := theme.Hint().Render("tab field · ←/→ cycle · enter save · esc cancel")
	if f.Busy {
		return []string{theme.WarningText().Render(theme.GlyphBusy + " working…"), hint}
	}
	out := make([]string, 0, len(f.Fields)*2+2)
	for i, fl := range f.Fields {
		label := theme.TextDim().Render(padTo(fl.Label, 10))
		mark := " "
		if i == f.cur {
			mark = string(theme.GlyphCursor)
		}
		var val string
		active := i == f.cur
		if len(fl.Toggle) > 0 {
			val = mark + "[" + fl.Toggle[fl.sel] + "]"
		} else if active {
			val = fl.cursorValue()
		} else {
			val = " " + fl.Value
		}
		if active {
			out = append(out, label+theme.TabActive().Render(val))
		} else {
			out = append(out, label+theme.TextDim().Render(val))
		}
	}
	if f.Err != "" {
		out = append(out, theme.DangerText().Render(theme.GlyphCross+" "+f.Err))
	}
	out = append(out, hint)
	return out
}

// cursorValue renders the value with the cursor block at the in-value
// position (end caret falls on the trailing block).
func (fl *Field) cursorValue() string {
	rs := []rune(fl.Value)
	cur := fl.cur
	if cur > len(rs) {
		cur = len(rs)
	}
	out := string(rs[:cur]) + string(theme.GlyphCursor)
	if cur < len(rs) {
		out += string(rs[cur:])
	}
	return out
}
