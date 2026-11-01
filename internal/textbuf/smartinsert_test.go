package textbuf

import "testing"

func typeInsert(e *Editor, keys ...string) {
	for _, k := range keys {
		e.Key(k)
	}
}

func TestSmartInsert(t *testing.T) {
	cases := []struct {
		name, unit string
		keys       []string
		want       string
	}{
		{"keeps indent", "\t", []string{"\t", "x", "enter", "y"}, "\tx\n\ty"},
		{"block between braces", "\t", []string{"f", "{", "enter", "z"}, "f{\n\tz\n}"},
		{"colon indents", "    ", []string{"i", "f", ":", "enter", "p"}, "if:\n    p"},
		{"paren pairs and types over", "\t", []string{"f", "(", "a", ")"}, "f(a)"},
		{"apostrophe does not pair", "\t", []string{"d", "o", "n", "'", "t"}, "don't"},
		{"quote pairs", "\t", []string{"=", " ", "\"", "x"}, "= \"x\""},
		{"backspace drops empty pair", "\t", []string{"(", "backspace", "z"}, "z"},
		{"soft tab backspace", "  ", []string{"tab", "tab", "backspace", "x"}, "  x"},
		{"tab inserts unit", "  ", []string{"tab", "x"}, "  x"},
	}
	for _, c := range cases {
		e := NewEditor("")
		e.SetIndent(c.unit)
		e.Key("i")
		for _, k := range c.keys {
			if k == "\t" {
				e.buf.InsertString("\t")
				continue
			}
			e.Key(k)
		}
		if got := e.Buffer().Text(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSmartInsertOff(t *testing.T) {
	e := NewEditor("")
	e.SetAutoPair(false)
	typeInsert(e, "i", "(", "x")
	if e.Buffer().Text() != "(x" {
		t.Fatal(e.Buffer().Text())
	}
}

func TestDetectIndent(t *testing.T) {
	for text, want := range map[string]string{
		"a\n\tb\n\tc":         "\t",
		"a\n  b\n    c\n  d":  "  ",
		"x\n    y\n        z": "    ",
		"plain\ntext":         "",
	} {
		if got := DetectIndent(text); got != want {
			t.Errorf("DetectIndent(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestVisualLineShiftAndDot(t *testing.T) {
	e := NewEditor("a\nb\nc\nd")
	e.SetIndent("  ")
	feed(e, "V", "j", "d")
	if got := e.Buffer().Text(); got != "c\nd" {
		t.Fatalf("Vjd = %q", got)
	}
	feed(e, "p")
	if got := e.Buffer().Text(); got != "c\na\nb\nd" {
		t.Fatalf("linewise paste = %q", got)
	}
	e = NewEditor("a\nb\nc")
	e.SetIndent("  ")
	feed(e, ">", ">")
	if got := e.Buffer().Text(); got != "  a\nb\nc" {
		t.Fatalf(">> = %q", got)
	}
	feed(e, "j", ".")
	if got := e.Buffer().Text(); got != "  a\n  b\nc" {
		t.Fatalf(". after >> = %q", got)
	}
	feed(e, "V", "k", "<")
	if got := e.Buffer().Text(); got != "a\nb\nc" {
		t.Fatalf("V< = %q", got)
	}
	feed(e, "2", ">", ">")
	if got := e.Buffer().Text(); got != "  a\n  b\nc" {
		t.Fatalf("2>> = %q", got)
	}
}

func TestDotRepeatsInsertAndDelete(t *testing.T) {
	e := NewEditor("x\ny\nz")
	feed(e, "A", "!", "esc", "j", ".")
	if got := e.Buffer().Text(); got != "x!\ny!\nz" {
		t.Fatalf("A! . = %q", got)
	}
	feed(e, "j", "d", "d", "k", ".")
	if got := e.Buffer().Text(); got != "y!" { // dd took z, k went to x!, . took it
		t.Fatalf("dd . = %q", got)
	}
	feed(e, "u")
	if got := e.Buffer().Text(); got != "x!\ny!" {
		t.Fatalf("u after . = %q", got)
	}
}
