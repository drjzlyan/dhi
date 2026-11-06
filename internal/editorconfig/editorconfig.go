// Package editorconfig reads .editorconfig files (editorconfig.org): the
// per-directory style rules many projects already ship (F-160). Read-only;
// it never writes and imports no other DHI package.
package editorconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// FileName is the file looked up in each directory.
const FileName = ".editorconfig"

// Config is the resolved properties for one file. Zero / nil means unset.
type Config struct {
	IndentStyle string // "tab", "space" or ""
	IndentSize  int    // 0 = unset; "tab" resolves to TabWidth
	TabWidth    int    // 0 = unset (defaults to a numeric indent_size)
	EndOfLine   string // "lf", "crlf", "cr" or ""
	Charset     string // "utf-8", "latin1", ... (only utf-8 is honoured)
	FinalNL     *bool  // insert_final_newline
	Trim        *bool  // trim_trailing_whitespace
}

// Section is one [glob] block.
type Section struct {
	Glob  string
	Props map[string]string
}

// File is a parsed .editorconfig.
type File struct {
	Root     bool
	Sections []Section
}

// Parse reads .editorconfig text. Comments are whole lines starting with
// '#' or ';'; keys and values are case-insensitive (stored lower-case).
func Parse(text string) File {
	var f File
	cur := -1
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && strings.HasSuffix(line, "]") {
			f.Sections = append(f.Sections, Section{Glob: line[1 : len(line)-1], Props: map[string]string{}})
			cur = len(f.Sections) - 1
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if cur < 0 {
			if k == "root" {
				f.Root = strings.EqualFold(v, "true")
			}
			continue
		}
		f.Sections[cur].Props[k] = strings.ToLower(v)
	}
	return f
}

// Match reports whether glob (a section header) selects rel, the
// slash-separated path of the file relative to the .editorconfig's folder.
// A glob without '/' matches at any depth; one with '/' is anchored.
func Match(glob, rel string) bool {
	if glob == "" {
		return false
	}
	g := glob
	switch {
	case strings.HasPrefix(g, "/"):
		g = g[1:]
	case !strings.Contains(g, "/"):
		g = "**/" + g
	}
	re, err := regexp.Compile("^" + translate(g) + "$")
	return err == nil && re.MatchString(rel)
}

// translate turns an editorconfig glob into regexp source.
func translate(g string) string {
	var b strings.Builder
	var open []bool // per open brace: a real {a,b} alternation (true) or literal
	rs := []rune(g)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case '\\':
			if i+1 < len(rs) {
				i++
				b.WriteString(regexp.QuoteMeta(string(rs[i])))
			}
		case '*':
			if i+1 < len(rs) && rs[i+1] == '*' {
				i++
				if i+1 < len(rs) && rs[i+1] == '/' { // "**/" also matches no directory
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			if j := classEnd(rs, i); j > 0 {
				b.WriteByte('[')
				k := i + 1
				if rs[k] == '!' {
					b.WriteByte('^')
					k++
				}
				for ; k < j; k++ {
					if rs[k] == '\\' || rs[k] == '[' {
						b.WriteByte('\\')
					}
					b.WriteRune(rs[k])
				}
				b.WriteByte(']')
				i = j
			} else {
				b.WriteString(`\[`)
			}
		case '{':
			if end := braceEnd(rs, i); end > 0 {
				if lo, hi, ok := numRange(string(rs[i+1 : end])); ok {
					b.WriteString(rangeRE(lo, hi))
					i = end
					continue
				}
				if hasTopComma(rs[i+1 : end]) {
					b.WriteString("(?:")
					open = append(open, true)
					continue
				}
			}
			b.WriteString(`\{`) // {single} and unmatched braces are literal
			open = append(open, false)
		case '}':
			if n := len(open); n > 0 {
				if open[n-1] {
					b.WriteByte(')')
				} else {
					b.WriteString(`\}`)
				}
				open = open[:n-1]
			} else {
				b.WriteString(`\}`)
			}
		case ',':
			if n := len(open); n > 0 && open[n-1] {
				b.WriteByte('|')
			} else {
				b.WriteByte(',')
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

func classEnd(rs []rune, i int) int {
	k := i + 1
	if k < len(rs) && rs[k] == '!' {
		k++
	}
	for j := k + 1; j < len(rs); j++ { // a class holds at least one char
		if rs[j] == ']' {
			return j
		}
		if rs[j] == '/' {
			return -1
		}
	}
	return -1
}

// braceEnd is the index of the '}' closing the '{' at i, or -1.
func braceEnd(rs []rune, i int) int {
	depth := 0
	for j := i; j < len(rs); j++ {
		switch rs[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

func hasTopComma(rs []rune) bool {
	depth := 0
	for j := 0; j < len(rs); j++ {
		switch rs[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func numRange(body string) (lo, hi int, ok bool) {
	a, z, found := strings.Cut(body, "..")
	if !found {
		return 0, 0, false
	}
	lo, e1 := strconv.Atoi(a)
	hi, e2 := strconv.Atoi(z)
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi-lo > 10000 {
		return 0, 0, false
	}
	return lo, hi, true
}

// rangeRE matches the integers lo..hi as an explicit alternation.
func rangeRE(lo, hi int) string {
	parts := make([]string, 0, hi-lo+1)
	for n := lo; n <= hi; n++ {
		parts = append(parts, strconv.Itoa(n))
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

// Resolver finds and caches .editorconfig files per directory.
type Resolver struct {
	mu    sync.Mutex
	cache map[string]*File // dir -> parsed file; nil = none there
}

// NewResolver returns an empty resolver.
func NewResolver() *Resolver { return &Resolver{cache: map[string]*File{}} }

// Invalidate drops every cached directory; call it after an .editorconfig
// was saved.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cache = map[string]*File{}
	r.mu.Unlock()
}

func (r *Resolver) load(dir string) *File {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.cache[dir]; ok {
		return f
	}
	var out *File
	if data, err := os.ReadFile(filepath.Join(dir, FileName)); err == nil {
		f := Parse(string(data))
		out = &f
	}
	r.cache[dir] = out
	return out
}

// Lookup resolves the properties for path: every .editorconfig from its
// folder up to stopDir (inclusive; "" means the file's own folder only),
// ending early at a file with root = true. Nearer files and later
// sections win.
func (r *Resolver) Lookup(path, stopDir string) Config {
	path = filepath.Clean(path)
	stop := ""
	if stopDir != "" {
		stop = filepath.Clean(stopDir)
	}
	var files []*File
	var dirs []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if f := r.load(dir); f != nil {
			files = append(files, f)
			dirs = append(dirs, dir)
			if f.Root {
				break
			}
		}
		if stop == "" || dir == stop || dir == filepath.Dir(dir) {
			break
		}
		if rel, err := filepath.Rel(stop, dir); err != nil || strings.HasPrefix(rel, "..") {
			break // never climb out of the member
		}
	}
	props := map[string]string{}
	for i := len(files) - 1; i >= 0; i-- { // farthest first, nearest overrides
		rel, err := filepath.Rel(dirs[i], path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, s := range files[i].Sections {
			if Match(s.Glob, rel) {
				for k, v := range s.Props {
					props[k] = v
				}
			}
		}
	}
	return resolve(props)
}

func resolve(p map[string]string) Config {
	get := func(k string) string {
		if v := p[k]; v != "unset" {
			return v
		}
		return ""
	}
	var c Config
	if v := get("indent_style"); v == "tab" || v == "space" {
		c.IndentStyle = v
	}
	if v, err := strconv.Atoi(get("tab_width")); err == nil && v > 0 {
		c.TabWidth = v
	}
	if v := get("indent_size"); v == "tab" {
		c.IndentSize = c.TabWidth
	} else if n, err := strconv.Atoi(v); err == nil && n > 0 {
		c.IndentSize = n
		if c.TabWidth == 0 {
			c.TabWidth = n // spec: tab_width defaults to indent_size
		}
	}
	if v := get("end_of_line"); v == "lf" || v == "crlf" || v == "cr" {
		c.EndOfLine = v
	}
	c.Charset = get("charset")
	c.FinalNL = boolProp(get("insert_final_newline"))
	c.Trim = boolProp(get("trim_trailing_whitespace"))
	return c
}

// IndentUnit is what Tab inserts under this config: a tab for tab style,
// spaces for space style (indent_size, else tab_width, else fallback), and
// spaces for a bare indent_size. "" when the config says nothing.
func (c Config) IndentUnit(fallback int) string {
	switch {
	case c.IndentStyle == "tab":
		return "\t"
	case c.IndentStyle == "space":
		n := c.IndentSize
		if n == 0 {
			n = c.TabWidth
		}
		if n == 0 {
			n = fallback
		}
		if n <= 0 {
			n = 4
		}
		return strings.Repeat(" ", min(n, 16))
	case c.IndentSize > 0:
		return strings.Repeat(" ", min(c.IndentSize, 16))
	}
	return ""
}

func boolProp(v string) *bool {
	switch v {
	case "true":
		t := true
		return &t
	case "false":
		f := false
		return &f
	}
	return nil
}
