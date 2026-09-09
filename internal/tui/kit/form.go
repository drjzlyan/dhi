package kit

import (
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Field is one form input: a text buffer or a cycling toggle choice.
type Field struct {
	Label  string
	Value  string   // text fields
	Toggle []string // non-empty: left/right cycles choices
	sel    int      // selected toggle index
}

// NewTextField builds a free-text field pre-filled with value.
func NewTextField(label, value string) Field { return Field{Label: label, Value: value} }

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

// Form is the canonical dialog form (F-024): labeled fields, tab to
// cycle, left/right cycle toggles, enter submits, esc cancels, busy
// swallows input. Submit handling stays with the surface — call
// Values/ToggleIndex inside your submit branch.
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
	case "backspace":
		fl := &f.Fields[f.cur]
		if len(fl.Toggle) == 0 && len(fl.Value) > 0 {
			fl.Value = fl.Value[:len(fl.Value)-1]
		}
		return FormNone
	case "left":
		f.cycleCur(-1)
		return FormNone
	case "right":
		f.cycleCur(1)
		return FormNone
	}
	fl := &f.Fields[f.cur]
	if len(fl.Toggle) == 0 {
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			fl.Value += key
		}
	}
	return FormNone
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
		if len(fl.Toggle) > 0 {
			val = mark + "[" + fl.Toggle[fl.sel] + "]"
		} else if i == f.cur {
			val = fl.Value
		} else {
			val = " " + fl.Value
		}
		out = append(out, label+theme.TabActive().Render(val))
	}
	if f.Err != "" {
		out = append(out, theme.DangerText().Render(theme.GlyphCross+" "+f.Err))
	}
	out = append(out, hint)
	return out
}
