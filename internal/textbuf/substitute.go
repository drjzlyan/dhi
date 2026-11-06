package textbuf

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Regex :s (F-137). The pattern is vim's default "magic" syntax mapped
// onto Go RE2; the replacement supports & and \0..\9.
//
// Supported: . * ^ $ [...] classes, \< \> (word edges), \( \) groups,
// \| \+ \? \= \{n,m}, \s \S \d \D \w \W \t \n, \/. Unescaped ( ) | + ? { }
// are literal, as in vim.
// Not supported (named error): \v \V \c \C \zs \ze \@ \% \_ and
// backreferences in the pattern (RE2 has none).
// Flags: g, i, I. The c (confirm) flag is refused by name.

// translateVimPattern turns a vim magic-mode pattern into RE2 syntax.
func translateVimPattern(pat string) (string, error) {
	rs := []rune(pat)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '(', ')', '|', '+', '?', '{', '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '[':
			j := classEnd(rs, i)
			if j < 0 { // vim treats an unclosed [ as a literal
				b.WriteString(`\[`)
				continue
			}
			b.WriteString(string(rs[i : j+1]))
			i = j
		case '\\':
			i++
			if i >= len(rs) {
				return "", fmt.Errorf("pattern ends with a backslash")
			}
			e := rs[i]
			switch e {
			case '<', '>':
				b.WriteString(`\b`)
			case '(', ')', '|', '+', '?', '{', '}':
				b.WriteRune(e)
			case '=':
				b.WriteByte('?')
			case 's', 'S', 'd', 'D', 'w', 'W', 't', 'n':
				b.WriteByte('\\')
				b.WriteRune(e)
			case '/':
				b.WriteByte('/')
			default:
				if e >= '1' && e <= '9' {
					return "", fmt.Errorf(`backreference \%c in the pattern is not supported`, e)
				}
				if unicode.IsLetter(e) || unicode.IsDigit(e) || e == '%' || e == '@' || e == '_' {
					return "", fmt.Errorf(`\%c is not supported in the pattern`, e)
				}
				b.WriteByte('\\')
				b.WriteRune(e)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), nil
}

// classEnd returns the index of the ] closing the class opened at rs[i],
// or -1 when it never closes.
func classEnd(rs []rune, i int) int {
	j := i + 1
	if j < len(rs) && rs[j] == '^' {
		j++
	}
	if j < len(rs) && rs[j] == ']' {
		j++
	}
	for j < len(rs) && rs[j] != ']' {
		switch {
		case rs[j] == '[' && j+1 < len(rs) && rs[j+1] == ':': // [:alpha:]
			if end := strings.Index(string(rs[j+2:]), ":]"); end >= 0 {
				j += 2 + len([]rune(string(rs[j+2:])[:end])) + 2
				continue
			}
		case rs[j] == '\\' && j+1 < len(rs):
			j++
		}
		j++
	}
	if j >= len(rs) {
		return -1
	}
	return j
}

// repPart is one piece of a parsed replacement: literal text, or a
// capture group (group >= 0; 0 is the whole match).
type repPart struct {
	lit   string
	group int
}

// parseReplacement reads &, \0..\9, \n and \r (line break), \t, \& and \\.
func parseReplacement(rep string) []repPart {
	var parts []repPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, repPart{lit: lit.String(), group: -1})
			lit.Reset()
		}
	}
	rs := []rune(rep)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '&':
			flush()
			parts = append(parts, repPart{group: 0})
		case r == '\\' && i+1 < len(rs):
			i++
			switch e := rs[i]; {
			case e >= '0' && e <= '9':
				flush()
				parts = append(parts, repPart{group: int(e - '0')})
			case e == 'n' || e == 'r':
				lit.WriteByte('\n')
			case e == 't':
				lit.WriteByte('\t')
			default:
				lit.WriteRune(e)
			}
		default:
			lit.WriteRune(r)
		}
	}
	flush()
	return parts
}

func maxGroup(parts []repPart) int {
	m := 0
	for _, p := range parts {
		m = max(m, p.group)
	}
	return m
}

// SubstituteRegex replaces matches of re with the parsed replacement on
// lines [from, to] (0-based, inclusive): the first match per line, or all
// when global. A replacement holding a line break splits the line.
// Returns (lines changed, hits). One undo step covers the whole call; the
// buffer is untouched when nothing matched.
func (b *Buffer) SubstituteRegex(re *regexp.Regexp, rep []repPart, from, to int, global bool) (lines, hits int) {
	from, to = max(from, 0), min(to, len(b.lines)-1)
	out := make([]string, 0, len(b.lines))
	out = append(out, b.lines[:from]...)
	n := 1
	if global {
		n = -1
	}
	for i := from; i <= to; i++ {
		line := b.lines[i]
		ms := re.FindAllStringSubmatchIndex(line, n)
		if len(ms) == 0 {
			out = append(out, line)
			continue
		}
		var sb strings.Builder
		last := 0
		for _, m := range ms {
			sb.WriteString(line[last:m[0]])
			for _, p := range rep {
				switch {
				case p.group < 0:
					sb.WriteString(p.lit)
				case 2*p.group+1 < len(m) && m[2*p.group] >= 0:
					sb.WriteString(line[m[2*p.group]:m[2*p.group+1]])
				}
			}
			last = m[1]
		}
		sb.WriteString(line[last:])
		out = append(out, strings.Split(sb.String(), "\n")...)
		lines++
		hits += len(ms)
	}
	if hits == 0 {
		return 0, 0
	}
	out = append(out, b.lines[to+1:]...)
	b.snapshotBefore()
	b.lines = out
	b.clampCursor()
	b.dirty = true
	b.seq++
	return lines, hits
}

// splitUnescaped splits s at sep; backslash pairs stay intact except that
// an escaped sep loses its backslash.
func splitUnescaped(s string, sep rune) []string {
	var parts []string
	var cur strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		switch {
		case rs[i] == '\\' && i+1 < len(rs):
			if rs[i+1] == sep {
				cur.WriteRune(sep)
			} else {
				cur.WriteRune(rs[i])
				cur.WriteRune(rs[i+1])
			}
			i++
		case rs[i] == sep:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(rs[i])
		}
	}
	return append(parts, cur.String())
}

// parseExAddr reads one line address off s: a number, ".", "$", "'<" or
// "'>". ok is false when s starts with none.
func (e *Editor) parseExAddr(s string) (line int, rest string, ok bool, err error) {
	last := e.buf.LineCount() - 1
	switch {
	case strings.HasPrefix(s, "'<"), strings.HasPrefix(s, "'>"):
		if !e.markSet {
			return 0, s, true, fmt.Errorf("no visual selection for '< and '>")
		}
		if s[1] == '<' {
			return min(e.markLo, last), s[2:], true, nil
		}
		return min(e.markHi, last), s[2:], true, nil
	case strings.HasPrefix(s, "."):
		return e.buf.Cursor().Line, s[1:], true, nil
	case strings.HasPrefix(s, "$"):
		return last, s[1:], true, nil
	}
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n == 0 {
		return 0, s, false, nil
	}
	v, _ := strconv.Atoi(s[:n])
	if v < 1 || v > last+1 {
		return 0, s, true, fmt.Errorf("invalid range: line %d (buffer has %d lines)", v, last+1)
	}
	return v - 1, s[n:], true, nil
}

// parseExRange reads an optional range off the front of cmd: "%", "n",
// "n,m" (see parseExAddr). It returns 0-based inclusive lines.
func (e *Editor) parseExRange(cmd string) (from, to int, rest string, hasRange bool, err error) {
	if strings.HasPrefix(cmd, "%") {
		return 0, e.buf.LineCount() - 1, cmd[1:], true, nil
	}
	a, rest, ok, err := e.parseExAddr(cmd)
	if err != nil || !ok {
		return 0, 0, cmd, ok, err
	}
	z := a
	if strings.HasPrefix(rest, ",") {
		var ok2 bool
		z, rest, ok2, err = e.parseExAddr(rest[1:])
		if err != nil {
			return 0, 0, cmd, true, err
		}
		if !ok2 {
			return 0, 0, cmd, true, fmt.Errorf("invalid range: missing line after comma")
		}
	}
	if a > z {
		return 0, 0, cmd, true, fmt.Errorf("backwards range: %d,%d", a+1, z+1)
	}
	return a, z, rest, true, nil
}

// isSepRune reports whether r can delimit a :s command (vim: any
// non-alphanumeric except space, backslash, quote and bar).
func isSepRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) && !strings.ContainsRune(`\"|`, r)
}

// subHead reports whether s starts a substitute (`s` + separator).
func subHead(s string) (sep rune, ok bool) {
	if !strings.HasPrefix(s, "s") || len(s) < 2 {
		return 0, false
	}
	sep = []rune(s[1:])[0]
	return sep, isSepRune(sep)
}

// exSubstitute implements `:[range]s/pat/rep/[flags]` (F-137; plain
// substring before). Returns false when cmd is not a substitute form.
func exSubstitute(e *Editor, cmd string) bool {
	from, to, rest, hasRange, err := e.parseExRange(cmd)
	if err != nil {
		// a bad range only counts as ours when a substitute follows it
		if i := strings.IndexAny(cmd, "s"); i >= 0 {
			if _, ok := subHead(cmd[i:]); ok {
				e.message = err.Error()
				return true
			}
		}
		return false
	}
	sep, ok := subHead(rest)
	if !ok {
		return false
	}
	if !hasRange {
		from = e.buf.Cursor().Line
		to = from
	}
	parts := splitUnescaped(rest[1+len(string(sep)):], sep)
	if len(parts) < 2 || len(parts) > 3 {
		e.message = "usage: [range]s/pat/rep/[gIi]"
		return true
	}
	pat, rep := parts[0], parts[1]
	global, fold := false, false
	if len(parts) == 3 {
		for _, f := range parts[2] {
			switch f {
			case 'g':
				global = true
			case 'i':
				fold = true
			case 'I':
				fold = false
			case 'c':
				e.message = "the c (confirm) flag is not supported"
				return true
			default:
				e.message = "unknown :s flag: " + string(f)
				return true
			}
		}
	}
	if pat == "" {
		e.message = "empty pattern"
		return true
	}
	src, err := translateVimPattern(pat)
	if err != nil {
		e.message = "invalid pattern: " + err.Error()
		return true
	}
	if fold {
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		e.message = "invalid pattern: " + err.Error()
		return true
	}
	rparts := parseReplacement(rep)
	if g := maxGroup(rparts); g > re.NumSubexp() {
		e.message = fmt.Sprintf(`replacement uses \%d but the pattern has %d group(s)`, g, re.NumSubexp())
		return true
	}
	replaced, n := e.buf.SubstituteRegex(re, rparts, from, to, global)
	if n == 0 {
		e.message = "pattern not found: " + pat
		return true
	}
	lines := " lines changed ("
	if replaced == 1 {
		lines = " line changed ("
	}
	hits := " hits)"
	if n == 1 {
		hits = " hit)"
	}
	e.message = itoa(replaced) + lines + itoa(n) + hits
	return true
}
