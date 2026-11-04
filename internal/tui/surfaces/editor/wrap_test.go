package editor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

// openText opens app.go holding text, with wrap on or off.
func openText(t *testing.T, text string, wrap bool) *Model {
	t.Helper()
	m := newEditor(t)
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.active() == nil {
		t.Fatal("app.go did not open")
	}
	m.prefs = func() Prefs { return Prefs{TabWidth: 4, LineNumbers: true, Wrap: wrap, Scrollback: 1000} }
	return m
}

// words is a long line of numbered words, wider than the pane.
func words(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("word")
		b.WriteString(string(rune('a' + i%26)))
	}
	return b.String()
}

// shownRows returns the buffer rows of the plain view that carry text.
func shownRows(m *Model) []string {
	var out []string
	for _, r := range strings.Split(plainView(m), "\n") {
		if strings.Contains(r, "word") || strings.Contains(r, "second") || strings.Contains(r, "short") {
			out = append(out, r)
		}
	}
	return out
}

func TestWrapSegments(t *testing.T) {
	cases := []struct {
		name  string
		plain string
		width int
		want  []int
	}{
		{"fits", "abc def", 10, []int{0}},
		{"exact fit stays one row", "abcdefghij", 10, []int{0}},
		{"breaks after a space", "hello world again", 12, []int{0, 12}},
		{"hard break in a long word", "abcdefghijklmno", 10, []int{0, 10}},
		{"wide runes stay whole", "日本語日本語", 5, []int{0, 2, 4}},
		{"empty", "", 10, []int{0}},
		{"indent is not a break point", "    abcdefghijklmnop", 10, []int{0, 10}},
	}
	for _, c := range cases {
		if got := wrapSegments(c.plain, c.width); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: wrapSegments(%q, %d) = %v, want %v", c.name, c.plain, c.width, got, c.want)
		}
	}
}

func TestWrapOffKeepsHorizontalScroll(t *testing.T) {
	m := openText(t, words(40)+"\nshort\n", false)
	if got := len(shownRows(m)); got != 2 { // the long line stays one row
		t.Fatalf("nowrap shows %d text rows, want 2:\n%s", got, plainView(m))
	}
	if m.bufGeom.rowLine != nil {
		t.Fatal("wrap geometry recorded with wrap off")
	}
}

func TestWrapContinuesLongLinesAndNumbersFirstRowOnly(t *testing.T) {
	m := openText(t, words(40)+"\nshort\n", true)
	buf := shownRows(m)
	if len(buf) < 3 {
		t.Fatalf("long line did not wrap:\n%s", plainView(m))
	}
	all := strings.Join(buf, "\n")
	if !strings.Contains(buf[0], " 1 ") || strings.Contains(buf[1], " 1 ") || strings.Contains(buf[1], " 2 ") {
		t.Fatalf("number belongs on the first row only:\n%s", all)
	}
	if !strings.Contains(buf[len(buf)-1], " 2 short") {
		t.Fatalf("next logical line keeps its own number:\n%s", all)
	}
	// Every word is on screen: nothing was cut off at the pane edge.
	for _, w := range []string{"worda", "wordz", "wordn"} {
		if !strings.Contains(all, w) {
			t.Errorf("%s missing:\n%s", w, all)
		}
	}
}

func TestWrapBreaksAtSpaces(t *testing.T) {
	m := openText(t, words(40)+"\n", true)
	for _, r := range shownRows(m) {
		rest := strings.TrimRight(r, " │")
		if !strings.HasSuffix(rest, "a") && !strings.HasSuffix(rest, "b") && len(rest) > 0 {
			// every word ends in a letter after "word", so a row ending
			// in "w", "wo", "wor" or "word" split a word.
			if strings.HasSuffix(rest, "wor") || strings.HasSuffix(rest, "wo") || strings.HasSuffix(rest, "word") {
				t.Fatalf("word split across rows: %q", r)
			}
		}
	}
}

func TestSetWrapAndNoWrapOverrideSettings(t *testing.T) {
	m := openText(t, words(40)+"\n", false)
	if n := len(shownRows(m)); n != 1 {
		t.Fatalf("nowrap shows one row, got %d", n)
	}
	feed(m, ":")
	typeKeys(m, "set wrap")
	feed(m, "enter")
	if len(shownRows(m)) < 2 || !strings.Contains(m.active().Message(), "wrap: on") {
		t.Fatalf(":set wrap did not wrap (msg %q):\n%s", m.active().Message(), plainView(m))
	}
	feed(m, ":")
	typeKeys(m, "set nowrap")
	feed(m, "enter")
	if n := len(shownRows(m)); n != 1 {
		t.Fatalf(":set nowrap did not unwrap (%d rows):\n%s", n, plainView(m))
	}
}

func TestWrapJKStayLogical(t *testing.T) {
	m := openText(t, words(40)+"\nsecond\nthird\n", true)
	e := m.active()
	feed(m, "j")
	if c := e.Buffer().Cursor(); c.Line != 1 {
		t.Fatalf("j moved to line %d, want 1 (a logical line, not the next row)", c.Line)
	}
	feed(m, "k")
	if c := e.Buffer().Cursor(); c.Line != 0 {
		t.Fatalf("k moved to line %d, want 0", c.Line)
	}
}

func TestWrapCursorOnLaterRowStaysVisible(t *testing.T) {
	m := openText(t, words(40)+"\nsecond\n", true)
	feed(m, "$")
	m.View()
	if g := m.bufGeom; g.rowLine[0] != 0 || len(g.rowLine) < 3 {
		t.Fatalf("geometry = %+v", g)
	}
	if !strings.Contains(plainView(m), "wordn") { // 40 words end on 'n'
		t.Fatalf("end of the long line not shown:\n%s", plainView(m))
	}
}

func TestWrapScrollsByRowsKeepingCursorInView(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString(words(30) + "\n")
	}
	m := openText(t, b.String(), true)
	feed(m, "G")
	for i := 0; i < 5; i++ {
		feed(m, "k")
	}
	line := m.active().Buffer().Cursor().Line
	m.View()
	found := false
	for _, l := range m.bufGeom.rowLine {
		found = found || l == line
	}
	if !found {
		t.Fatalf("cursor line %d not among shown rows %v", line, m.bufGeom.rowLine)
	}
	if n := len(m.bufGeom.rowLine); n > m.height-5 {
		t.Fatalf("%d rows exceed the %d available", n, m.height-5)
	}
}

func TestWrapCursorLineTallerThanPane(t *testing.T) {
	m := openText(t, words(900)+"\n", true)
	feed(m, "$")
	m.View()
	if n := len(m.bufGeom.rowLine); n > m.height-5 || n == 0 {
		t.Fatalf("rows = %d", n)
	}
	if m.bufGeom.rowStart[len(m.bufGeom.rowStart)-1] == 0 {
		t.Fatal("the window did not follow the cursor to the last row")
	}
}

func TestWrapClickMapsRowToPosition(t *testing.T) {
	m := openText(t, words(40)+"\nsecond\n", true)
	m.View()
	g := m.bufGeom
	if !g.ok || len(g.rowStart) < 3 {
		t.Fatalf("geometry = %+v", g)
	}
	// Second row, third cell of the text.
	p, ok := m.bufferPos(g.ox+g.textX+2, g.oy+1)
	if !ok || p.Line != 0 || p.Col != g.rowStart[1]+2 {
		t.Fatalf("click on a continuation row = %+v ok=%v, want line 0 col %d", p, ok, g.rowStart[1]+2)
	}
	last := len(g.rowLine) - 2 // "second"; the file's final empty line is below it
	p, ok = m.bufferPos(g.ox+g.textX+1, g.oy+last)
	if !ok || p.Line != 1 || p.Col != 1 {
		t.Fatalf("click on the next line = %+v ok=%v", p, ok)
	}
}

func TestWrapWideRunesStayWhole(t *testing.T) {
	m := openText(t, strings.Repeat("日本語", 30)+"\n", true)
	v := plainView(m)
	if strings.Count(v, "日") != 30 || strings.Count(v, "本") != 30 || strings.Count(v, "語") != 30 {
		t.Fatalf("wide runes lost or duplicated:\n%s", v)
	}
	feed(m, "$")
	m.View()
	if len(m.bufGeom.rowLine) < 2 {
		t.Fatal("wide line did not wrap")
	}
}

func TestWrapEndOfLineCursorGetsACell(t *testing.T) {
	// A line exactly one pane wide: insert mode at the end needs a cell
	// for the cursor block, so it moves to a second row.
	m := openText(t, "x\n", true)
	m.View()
	w := m.bufTextWidth(len("1") + 1)
	feed(m, "A")
	typeKeys(m, strings.Repeat("y", w-1))
	m.View()
	if rows := len(m.bufGeom.rowLine); rows != 3 { // line 1 (2 rows) + the empty last line
		t.Fatalf("full line with the end cursor takes %d rows (incl. the empty line), want 3", rows)
	}
}

func TestWrapGolden(t *testing.T) {
	m := openText(t, "package main\n\n// "+words(45)+"\n\nfunc main() {\n\tprintln(\""+strings.Repeat("日本語", 40)+"\")\n}\n", true)
	feed(m, "j", "j")
	golden.Snapshot(t, "editor_wrap_100x30", m.View())
}
