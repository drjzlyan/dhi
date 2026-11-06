package textbuf

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Snippets (F-171): the LSP / TextMate snippet grammar, a pure parser and
// expander plus an insert-mode tab-stop session.
//
// Supported: $1  ${1}  ${1:default}  ${1|a,b,c|} (first choice is inserted)
// $0, nested placeholders, \$ \} \\ escapes, $NAME / ${NAME} /
// ${NAME:default} variables. Unsupported: ${name/regex/format/} transforms;
// the transform is dropped and the plain value (or tab stop) is used.

// Span is a half-open rune range [Start, End).
type Span struct{ Start, End int }

// SnippetStop is one numbered tab stop; mirrored stops have several spans.
type SnippetStop struct {
	N     int
	Spans []Span // sorted by Start
}

// Snippet is an expanded snippet: plain text plus its tab stops in visit
// order (1, 2, ... then 0, the final cursor position).
type Snippet struct {
	Text  string
	Stops []SnippetStop
}

type snKind uint8

const (
	snText snKind = iota
	snStop
	snVar
)

type snNode struct {
	kind snKind
	text string // snText
	n    int    // snStop
	name string // snVar
	kids []snNode
}

// ExpandSnippet parses src and expands it. vars resolves $NAME variables
// (nil = none known); indent is appended after every newline of the
// snippet so continuation lines match the line it lands on.
func ExpandSnippet(src string, vars func(name string) (string, bool), indent string) Snippet {
	nodes, _ := snParseSeq([]rune(src), 0, false)
	r := &snRender{vars: vars, indent: indent, defs: map[int][]snNode{}, spans: map[int][]Span{}}
	r.collect(nodes)
	r.render(nodes, 0)
	sn := Snippet{Text: string(r.out)}
	var nums []int
	for n := range r.spans {
		if n != 0 {
			nums = append(nums, n)
		}
	}
	for i := 1; i < len(nums); i++ { // insertion sort: tiny
		for j := i; j > 0 && nums[j] < nums[j-1]; j-- {
			nums[j], nums[j-1] = nums[j-1], nums[j]
		}
	}
	for _, n := range nums {
		sn.Stops = append(sn.Stops, SnippetStop{N: n, Spans: sortSpans(r.spans[n])})
	}
	final := r.spans[0]
	if len(final) == 0 {
		final = []Span{{len(r.out), len(r.out)}}
	}
	sn.Stops = append(sn.Stops, SnippetStop{N: 0, Spans: sortSpans(final)})
	return sn
}

func sortSpans(in []Span) []Span {
	out := append([]Span(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Start < out[j-1].Start; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isVarStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// snParseSeq reads nodes from i until the end or, inside a placeholder,
// the closing brace (left unconsumed). The second result is the new index.
func snParseSeq(r []rune, i int, inBrace bool) ([]snNode, int) {
	var nodes []snNode
	var text []rune
	flush := func() {
		if len(text) > 0 {
			nodes = append(nodes, snNode{kind: snText, text: string(text)})
			text = nil
		}
	}
	for i < len(r) {
		c := r[i]
		switch {
		case c == '}' && inBrace:
			flush()
			return nodes, i
		case c == '\\' && i+1 < len(r) && (r[i+1] == '$' || r[i+1] == '}' || r[i+1] == '\\'):
			text = append(text, r[i+1])
			i += 2
		case c == '$':
			node, next, ok := snParseDollar(r, i)
			if !ok {
				text = append(text, c)
				i++
				continue
			}
			flush()
			nodes = append(nodes, node)
			i = next
		default:
			text = append(text, c)
			i++
		}
	}
	flush()
	return nodes, i
}

// snParseDollar parses a $... form at r[i]; ok=false means a literal $.
func snParseDollar(r []rune, i int) (snNode, int, bool) {
	j := i + 1
	if j >= len(r) {
		return snNode{}, 0, false
	}
	if isDigit(r[j]) {
		n, k := snNumber(r, j)
		return snNode{kind: snStop, n: n}, k, true
	}
	if isVarStart(r[j]) {
		k := snName(r, j)
		return snNode{kind: snVar, name: string(r[j:k])}, k, true
	}
	if r[j] != '{' {
		return snNode{}, 0, false
	}
	j++
	if j >= len(r) {
		return snNode{}, 0, false
	}
	var node snNode
	switch {
	case isDigit(r[j]):
		n, k := snNumber(r, j)
		node = snNode{kind: snStop, n: n}
		j = k
	case isVarStart(r[j]):
		k := snName(r, j)
		node = snNode{kind: snVar, name: string(r[j:k])}
		j = k
	default:
		return snNode{}, 0, false
	}
	if j >= len(r) {
		return snNode{}, 0, false
	}
	switch r[j] {
	case '}':
		return node, j + 1, true
	case ':':
		kids, k := snParseSeq(r, j+1, true)
		if k >= len(r) {
			return snNode{}, 0, false
		}
		node.kids = kids
		return node, k + 1, true
	case '|':
		if node.kind != snStop {
			return snNode{}, 0, false
		}
		first, k, ok := snChoice(r, j+1)
		if !ok {
			return snNode{}, 0, false
		}
		node.kids = []snNode{{kind: snText, text: first}}
		return node, k, true
	case '/': // transform: dropped
		k := j
		for k < len(r) && r[k] != '}' {
			if r[k] == '\\' {
				k++
			}
			k++
		}
		if k >= len(r) {
			return snNode{}, 0, false
		}
		return node, k + 1, true
	}
	return snNode{}, 0, false
}

func snNumber(r []rune, j int) (int, int) {
	n := 0
	for j < len(r) && isDigit(r[j]) {
		n = n*10 + int(r[j]-'0')
		j++
	}
	return n, j
}

func snName(r []rune, j int) int {
	for j < len(r) && (isVarStart(r[j]) || isDigit(r[j])) {
		j++
	}
	return j
}

// snChoice reads `a,b,c|}` after the opening `|` and returns the first
// choice and the index past the closing brace.
func snChoice(r []rune, j int) (string, int, bool) {
	var first []rune
	inFirst := true
	for j < len(r) {
		switch {
		case r[j] == '\\' && j+1 < len(r):
			if inFirst {
				first = append(first, r[j+1])
			}
			j += 2
		case r[j] == ',':
			inFirst = false
			j++
		case r[j] == '|' && j+1 < len(r) && r[j+1] == '}':
			return string(first), j + 2, true
		default:
			if inFirst {
				first = append(first, r[j])
			}
			j++
		}
	}
	return "", 0, false
}

type snRender struct {
	vars   func(string) (string, bool)
	indent string
	out    []rune
	defs   map[int][]snNode // first non-empty default per stop number
	spans  map[int][]Span
}

// collect finds each stop's default text so a bare $1 mirrors ${1:text}.
func (r *snRender) collect(nodes []snNode) {
	for _, n := range nodes {
		if n.kind == snStop && len(n.kids) > 0 {
			if _, ok := r.defs[n.n]; !ok {
				r.defs[n.n] = n.kids
			}
		}
		if len(n.kids) > 0 {
			r.collect(n.kids)
		}
	}
}

func (r *snRender) write(s string) {
	if r.indent != "" {
		s = strings.ReplaceAll(s, "\n", "\n"+r.indent)
	}
	r.out = append(r.out, []rune(s)...)
}

func (r *snRender) render(nodes []snNode, depth int) {
	for _, n := range nodes {
		switch n.kind {
		case snText:
			r.write(n.text)
		case snStop:
			start := len(r.out)
			kids := n.kids
			if len(kids) == 0 && depth < 8 {
				kids = r.defs[n.n]
			}
			if depth < 8 {
				r.render(kids, depth+1)
			}
			r.spans[n.n] = append(r.spans[n.n], Span{start, len(r.out)})
		case snVar:
			if r.vars != nil {
				if v, ok := r.vars(n.name); ok {
					r.write(v)
					continue
				}
			}
			if len(n.kids) > 0 && depth < 8 {
				r.render(n.kids, depth+1)
			} else {
				r.write(n.name)
			}
		}
	}
}

// SnippetSession is the live tab-stop state after a snippet was expanded
// into a buffer. Spans are absolute rune offsets into Buffer.Text().
type SnippetSession struct {
	e     *Editor
	stops []SnippetStop
	cur   int
	prev  []rune // buffer text at the last sync
	sel   bool   // the placeholder text is "selected": typing replaces it
}

// Progress is the current stop's position, e.g. "2/3" (the final $0 not counted).
func (s *SnippetSession) Progress() string {
	total := len(s.stops) - 1
	return strconv.Itoa(min(s.cur+1, total)) + "/" + strconv.Itoa(total)
}

// Editor is the editor the session belongs to.
func (s *SnippetSession) Editor() *Editor { return s.e }

func (b *Buffer) offsetOf(p Pos) int {
	off := 0
	for i := 0; i < p.Line && i < len(b.lines); i++ {
		off += utf8.RuneCountInString(b.lines[i]) + 1
	}
	return off + p.Col
}

func (b *Buffer) posOfOffset(off int) Pos {
	for i, l := range b.lines {
		n := utf8.RuneCountInString(l)
		if off <= n {
			return Pos{Line: i, Col: off}
		}
		off -= n + 1
	}
	last := len(b.lines) - 1
	return Pos{Line: last, Col: utf8.RuneCountInString(b.lines[last])}
}

// InsertSnippet replaces start..end with the snippet and, when it has
// numbered stops, returns the session positioned on the first one (nil
// when there is nothing to tab through; the cursor then sits at $0).
func (e *Editor) InsertSnippet(start, end Pos, sn Snippet) *SnippetSession {
	b := e.buf
	base := b.offsetOf(start)
	b.BeginUndoGroup()
	b.ApplyEdit(start, end, sn.Text)
	b.EndUndoGroup()
	stops := make([]SnippetStop, len(sn.Stops))
	for i, st := range sn.Stops {
		stops[i] = SnippetStop{N: st.N, Spans: make([]Span, len(st.Spans))}
		for j, sp := range st.Spans {
			stops[i].Spans[j] = Span{base + sp.Start, base + sp.End}
		}
	}
	s := &SnippetSession{e: e, stops: stops, prev: []rune(b.Text())}
	if len(stops) == 1 {
		s.place(0)
		return nil
	}
	s.place(0)
	return s
}

// place puts the cursor on stop idx (end of its first span); a non-empty
// span is "selected" so the next typed key replaces it.
func (s *SnippetSession) place(idx int) {
	s.cur = idx
	sp := s.stops[idx].Spans[0]
	s.e.buf.SetCursor(s.e.buf.posOfOffset(sp.End))
	s.sel = sp.End > sp.Start
}

// BeforeKey runs before the editor sees key in insert mode. It returns
// true when the session consumed the key (tab / shift+tab jumps, or
// backspace/delete over a selected placeholder). done reports the session
// ended (esc, or the final stop was reached).
func (s *SnippetSession) BeforeKey(key string) (consumed, done bool) {
	switch key {
	case "tab", "shift+tab":
		step := 1
		if key == "shift+tab" {
			step = -1
		}
		idx := s.cur + step
		if idx < 0 {
			idx = 0
		}
		s.place(idx)
		return true, s.stops[idx].N == 0
	case "esc", "ctrl+[":
		return false, true
	}
	if s.sel {
		s.sel = false
		switch {
		case key == "backspace" || key == "delete":
			s.clearActive()
			return true, !s.Sync()
		case key == "enter" || strings.HasPrefix(key, "paste:") || (utf8.RuneCountInString(key) == 1 && key >= " "):
			s.clearActive()
			if !s.Sync() {
				return false, true
			}
		}
	}
	return false, false
}

// clearActive deletes the active stop's first span text (the placeholder).
func (s *SnippetSession) clearActive() {
	sp := s.stops[s.cur].Spans[0]
	b := s.e.buf
	if sp.End > sp.Start {
		b.ApplyEdit(b.posOfOffset(sp.Start), b.posOfOffset(sp.End), "")
	}
	b.SetCursor(b.posOfOffset(sp.Start))
}

// Sync folds the edit the last key made into the span offsets and mirrors
// the active stop's text into its other spans. It reports false when the
// user left the active stop (the session is over).
func (s *SnippetSession) Sync() bool {
	b := s.e.buf
	cur := []rune(b.Text())
	old := s.prev
	if string(cur) == string(old) {
		return s.cursorInside()
	}
	p := 0
	for p < len(old) && p < len(cur) && old[p] == cur[p] {
		p++
	}
	q := 0
	for q < len(old)-p && q < len(cur)-p && old[len(old)-1-q] == cur[len(cur)-1-q] {
		q++
	}
	a, z, n := p, len(old)-q, len(cur)-q-p
	owner := -1
	for j, sp := range s.stops[s.cur].Spans {
		if sp.Start <= a && z <= sp.End {
			owner = j
			break
		}
	}
	if owner < 0 {
		return false
	}
	s.transform(a, z, n, s.cur, owner)
	s.prev = cur
	s.mirror(owner)
	return s.cursorInside()
}

func (s *SnippetSession) cursorInside() bool {
	off := s.e.buf.offsetOf(s.e.buf.Cursor())
	for _, sp := range s.stops[s.cur].Spans {
		if off >= sp.Start && off <= sp.End {
			return true
		}
	}
	return false
}

// transform moves every span for the edit old[a:z] -> n runes. The span
// (os, oj) owns the edit and grows to hold it.
func (s *SnippetSession) transform(a, z, n, os, oj int) {
	delta := n - (z - a)
	for i := range s.stops {
		for j := range s.stops[i].Spans {
			sp := &s.stops[i].Spans[j]
			owned := i == os && j == oj
			switch {
			case sp.Start <= a && z <= sp.End && (owned || sp.Start < a || z < sp.End):
				sp.End += delta
			case sp.Start >= z:
				sp.Start += delta
				sp.End += delta
			case sp.End <= a:
			default: // the edit tore through it
				sp.Start, sp.End = a, a
			}
		}
	}
}

// mirror copies the owner span's text into the active stop's other spans.
func (s *SnippetSession) mirror(owner int) {
	b := s.e.buf
	spans := s.stops[s.cur].Spans
	if len(spans) < 2 {
		return
	}
	want := string(s.prev[spans[owner].Start:spans[owner].End])
	curOff := b.offsetOf(b.Cursor())
	for j := len(spans) - 1; j >= 0; j-- { // bottom-up: earlier spans stay valid
		if j == owner {
			continue
		}
		sp := s.stops[s.cur].Spans[j]
		if string(s.prev[sp.Start:sp.End]) == want {
			continue
		}
		n := utf8.RuneCountInString(want)
		b.BeginUndoGroup()
		b.ApplyEdit(b.posOfOffset(sp.Start), b.posOfOffset(sp.End), want)
		b.EndUndoGroup()
		s.prev = []rune(b.Text())
		s.transform(sp.Start, sp.End, n, s.cur, j)
		if curOff >= sp.End {
			curOff += n - (sp.End - sp.Start)
		}
	}
	b.SetCursor(b.posOfOffset(curOff))
}
