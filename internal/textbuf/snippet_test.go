package textbuf

import (
	"reflect"
	"testing"
)

func TestExpandSnippetTable(t *testing.T) {
	vars := func(n string) (string, bool) {
		switch n {
		case "TM_FILENAME":
			return "main.go", true
		case "EMPTY":
			return "", true
		}
		return "", false
	}
	cases := []struct {
		name, src, text string
		stops           []SnippetStop
	}{
		{"plain", "hello", "hello", []SnippetStop{{0, []Span{{5, 5}}}}},
		{"tabstops", "a $1 b $2", "a  b ", []SnippetStop{{1, []Span{{2, 2}}}, {2, []Span{{5, 5}}}, {0, []Span{{5, 5}}}}},
		{"placeholder", "f(${1:x}, ${2:yy})$0", "f(x, yy)",
			[]SnippetStop{{1, []Span{{2, 3}}}, {2, []Span{{5, 7}}}, {0, []Span{{8, 8}}}}},
		{"order", "$2 $1", " ", []SnippetStop{{1, []Span{{1, 1}}}, {2, []Span{{0, 0}}}, {0, []Span{{1, 1}}}}},
		{"choice", "${1|up,down|}", "up", []SnippetStop{{1, []Span{{0, 2}}}, {0, []Span{{2, 2}}}}},
		{"nested", "${1:a ${2:b}}", "a b", []SnippetStop{{1, []Span{{0, 3}}}, {2, []Span{{2, 3}}}, {0, []Span{{3, 3}}}}},
		{"mirror", "${1:x} = $1", "x = x", []SnippetStop{{1, []Span{{0, 1}, {4, 5}}}, {0, []Span{{5, 5}}}}},
		{"mirror before", "$1 ${1:x}", "x x", []SnippetStop{{1, []Span{{0, 1}, {2, 3}}}, {0, []Span{{3, 3}}}}},
		{"braced", "${1}", "", []SnippetStop{{1, []Span{{0, 0}}}, {0, []Span{{0, 0}}}}},
		{"escapes", `\$1 \} \\ \x`, `$1 } \ \x`, []SnippetStop{{0, []Span{{9, 9}}}}},
		{"escape in default", `${1:a\}b}`, "a}b", []SnippetStop{{1, []Span{{0, 3}}}, {0, []Span{{3, 3}}}}},
		{"var", "$TM_FILENAME ${TM_FILENAME}", "main.go main.go", []SnippetStop{{0, []Span{{15, 15}}}}},
		{"empty var wins over default", "${EMPTY:d}", "", []SnippetStop{{0, []Span{{0, 0}}}}},
		{"unknown var default", "${NOPE:dflt}", "dflt", []SnippetStop{{0, []Span{{4, 4}}}}},
		{"unknown var name", "$NOPE", "NOPE", []SnippetStop{{0, []Span{{4, 4}}}}},
		{"transform dropped", "${1/(.*)/$1/}x", "x", []SnippetStop{{1, []Span{{0, 0}}}, {0, []Span{{1, 1}}}}},
		{"lone dollar", "cost $ 5 ${", "cost $ 5 ${", []SnippetStop{{0, []Span{{11, 11}}}}},
		{"unclosed placeholder", "${1:abc", "${1:abc", []SnippetStop{{0, []Span{{7, 7}}}}},
		{"unicode offsets", "é${1:ü}", "éü", []SnippetStop{{1, []Span{{1, 2}}}, {0, []Span{{2, 2}}}}},
		{"explicit final", "a$0b$1", "ab", []SnippetStop{{1, []Span{{2, 2}}}, {0, []Span{{1, 1}}}}},
	}
	for _, c := range cases {
		got := ExpandSnippet(c.src, vars, "")
		if got.Text != c.text || !reflect.DeepEqual(got.Stops, c.stops) {
			t.Errorf("%s: %q\n got  %q %+v\n want %q %+v", c.name, c.src, got.Text, got.Stops, c.text, c.stops)
		}
	}
}

func TestExpandSnippetIndentsContinuationLines(t *testing.T) {
	got := ExpandSnippet("if x {\n\t$1\n}", nil, "  ")
	if got.Text != "if x {\n  \t\n  }" {
		t.Fatalf("%q", got.Text)
	}
	if got.Stops[0].Spans[0] != (Span{10, 10}) { // offsets include the indent
		t.Fatalf("%+v", got.Stops)
	}
}

// snipEditor opens content, enters insert mode at the end of the first
// line and inserts src there.
func snipEditor(t *testing.T, content, src string) (*Editor, *SnippetSession) {
	t.Helper()
	e := NewEditor(content)
	e.Key("A")
	cur := e.Buffer().Cursor()
	sn := ExpandSnippet(src, nil, "")
	return e, e.InsertSnippet(cur, cur, sn)
}

// typeIn feeds keys through the session the way the surface does.
func typeIn(e *Editor, s *SnippetSession, keys ...string) *SnippetSession {
	for _, k := range keys {
		if s != nil {
			consumed, done := s.BeforeKey(k)
			if done {
				if !consumed {
					e.Key(k)
				}
				return nil
			}
			if consumed {
				continue
			}
		}
		e.Key(k)
		if s != nil && !s.Sync() {
			s = nil
		}
	}
	return s
}

func TestSnippetSessionTabsAndReplacesPlaceholder(t *testing.T) {
	e, s := snipEditor(t, "x", "f(${1:aa}, ${2:bb})$0")
	if s == nil || e.Buffer().Text() != "xf(aa, bb)" {
		t.Fatalf("%v %q", s, e.Buffer().Text())
	}
	if c := e.Buffer().Cursor(); c.Col != 5 { // end of the first placeholder
		t.Fatalf("cursor %+v", c)
	}
	s = typeIn(e, s, "Z") // typing replaces the selected placeholder
	if e.Buffer().Text() != "xf(Z, bb)" {
		t.Fatalf("%q", e.Buffer().Text())
	}
	s = typeIn(e, s, "Y", "tab")
	if e.Buffer().Text() != "xf(ZY, bb)" || e.Buffer().Cursor().Col != 9 {
		t.Fatalf("%q %+v", e.Buffer().Text(), e.Buffer().Cursor())
	}
	s = typeIn(e, s, "shift+tab") // back; the placeholder is selected again
	s = typeIn(e, s, "backspace")
	if e.Buffer().Text() != "xf(, bb)" {
		t.Fatalf("backspace over a selected placeholder: %q", e.Buffer().Text())
	}
	s = typeIn(e, s, "tab", "tab") // second stop, then $0 ends the session
	if s != nil {
		t.Fatal("reaching $0 must end the session")
	}
	if c := e.Buffer().Cursor(); c.Col != 8 || e.Mode() != ModeInsert {
		t.Fatalf("final cursor %+v mode %v", c, e.Mode())
	}
}

func TestSnippetMirrorsUpdateTogether(t *testing.T) {
	e, s := snipEditor(t, "", "${1:v} := $1; use($1)\n$0")
	s = typeIn(e, s, "a", "b")
	if got := e.Buffer().Text(); got != "ab := ab; use(ab)\n" {
		t.Fatalf("%q", got)
	}
	if c := e.Buffer().Cursor(); c.Line != 0 || c.Col != 2 {
		t.Fatalf("cursor stays in the typed placeholder: %+v", c)
	}
	typeIn(e, s, "backspace")
	if got := e.Buffer().Text(); got != "a := a; use(a)\n" {
		t.Fatalf("%q", got)
	}
}

func TestSnippetNestedAndGrowth(t *testing.T) {
	e, s := snipEditor(t, "", "${1:a ${2:b}} $3")
	s = typeIn(e, s, "tab") // into the nested stop
	s = typeIn(e, s, "Q")
	if e.Buffer().Text() != "a Q " {
		t.Fatalf("%q", e.Buffer().Text())
	}
	s = typeIn(e, s, "tab", "x")
	if e.Buffer().Text() != "a Q x" {
		t.Fatalf("%q", e.Buffer().Text())
	}
	if s == nil {
		t.Fatal("session should still be live")
	}
}

func TestSnippetSessionEnds(t *testing.T) {
	e, s := snipEditor(t, "", "a${1:b}c ${2:d}")
	if s = typeIn(e, s, "esc"); s != nil || e.Mode() != ModeNormal {
		t.Fatalf("esc ends the session and the insert: %v %v", s, e.Mode())
	}
	e, s = snipEditor(t, "", "a${1:b}c ${2:d}")
	if s = typeIn(e, s, "Z", "right", "right", "right"); s != nil {
		t.Fatal("moving the cursor out of the stop ends the session")
	}
}

func TestSnippetOnlyFinalStopHasNoSession(t *testing.T) {
	e, s := snipEditor(t, "", "hello$0 world")
	if s != nil || e.Buffer().Cursor().Col != 5 {
		t.Fatalf("%v %+v", s, e.Buffer().Cursor())
	}
}

func TestSnippetShiftTabAtFirstStopStays(t *testing.T) {
	e, s := snipEditor(t, "", "${1:a} ${2:b}")
	s = typeIn(e, s, "shift+tab")
	if s == nil || s.Progress() != "1/2" || e.Buffer().Cursor().Col != 1 {
		t.Fatalf("%v %+v", s, e.Buffer().Cursor())
	}
}

func TestSnippetUndoesAsOneStep(t *testing.T) {
	e, _ := snipEditor(t, "x", "f(${1:a})")
	e.Key("esc")
	e.Key("u")
	if got := e.Buffer().Text(); got == "xf(a)" {
		t.Fatalf("snippet insertion not undoable: %q", got)
	}
}
