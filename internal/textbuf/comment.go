package textbuf

import (
	"path/filepath"
	"strings"
)

// Comment toggling (F-150): the comment syntax of a file type, the pure
// line transform, and the editor commands built on it (gcc, gc{motion},
// visual gc, ctrl+/).

// Comment is the comment syntax of one file type. Line is the line-comment
// token; Open and Close are the block pair, used per line when the type has
// no line comment (CSS, HTML).
type Comment struct {
	Line        string
	Open, Close string
}

var (
	slashes = Comment{Line: "//"}
	hash    = Comment{Line: "#"}
	dashes  = Comment{Line: "--"}
	semi    = Comment{Line: ";"}
	cBlock  = Comment{Open: "/*", Close: "*/"}
	htmlCmt = Comment{Open: "<!--", Close: "-->"}
)

var commentByExt = map[string]Comment{
	".go": slashes, ".ts": slashes, ".tsx": slashes, ".js": slashes, ".jsx": slashes,
	".mjs": slashes, ".cjs": slashes, ".jsonc": slashes, ".rs": slashes, ".c": slashes,
	".h": slashes, ".cc": slashes, ".cpp": slashes, ".hpp": slashes, ".java": slashes,
	".kt": slashes, ".swift": slashes, ".cs": slashes, ".scala": slashes, ".dart": slashes,
	".php": slashes, ".scss": slashes, ".less": slashes, ".proto": slashes, ".zig": slashes,
	".py": hash, ".pyi": hash, ".sh": hash, ".bash": hash, ".zsh": hash, ".rb": hash,
	".yaml": hash, ".yml": hash, ".toml": hash, ".mk": hash, ".pl": hash, ".r": hash,
	".tf": hash, ".conf": hash, ".env": hash, ".gitignore": hash,
	".lua": dashes, ".sql": dashes, ".hs": dashes,
	".lisp": semi, ".clj": semi, ".el": semi, ".ini": semi,
	".css":  cBlock,
	".html": htmlCmt, ".htm": htmlCmt, ".xml": htmlCmt, ".md": htmlCmt, ".svg": htmlCmt,
	".vue": htmlCmt, ".svelte": htmlCmt,
}

var commentByName = map[string]Comment{
	"makefile": hash, "dockerfile": hash, "justfile": hash, "gemfile": hash, "rakefile": hash,
	".gitignore": hash, ".env": hash,
}

// CommentFor is the comment syntax of the file at path, by extension (or
// by name for Makefile, Dockerfile…). ok is false for a type without
// comments (JSON) or one DHI does not know.
func CommentFor(path string) (Comment, bool) {
	base := strings.ToLower(filepath.Base(path))
	if c, ok := commentByName[base]; ok {
		return c, true
	}
	c, ok := commentByExt[filepath.Ext(base)]
	return c, ok
}

// CommentAction says what ToggleComment did.
type CommentAction uint8

const (
	CommentNone    CommentAction = iota // only blank lines: nothing to do
	CommentAdded                        // every non-blank line is now commented
	CommentRemoved                      // every non-blank line was commented and is not now
)

// ToggleComment comments or uncomments lines, like vim-commentary and
// VS Code. When every non-blank line is already commented they are all
// uncommented (the token and one space after it go); otherwise every
// non-blank line gets the comment at the shallowest indent of the range.
// Blank lines are untouched. It never modifies its input.
func ToggleComment(lines []string, c Comment) ([]string, CommentAction) {
	out := append([]string(nil), lines...)
	var idx []int
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 || (c.Line == "" && (c.Open == "" || c.Close == "")) {
		return out, CommentNone
	}
	all := true
	for _, i := range idx {
		if !isCommented(lines[i], c) {
			all = false
			break
		}
	}
	if all {
		for _, i := range idx {
			out[i] = uncommentLine(lines[i], c)
		}
		return out, CommentRemoved
	}
	pad := commonIndent(lines, idx)
	for _, i := range idx {
		rest := lines[i][len(pad):]
		if c.Line != "" {
			out[i] = pad + c.Line + " " + rest
		} else {
			out[i] = pad + c.Open + " " + strings.TrimRight(rest, " \t") + " " + c.Close
		}
	}
	return out, CommentAdded
}

func isCommented(line string, c Comment) bool {
	t := strings.TrimSpace(line)
	if c.Line != "" {
		return strings.HasPrefix(t, c.Line)
	}
	return len(t) >= len(c.Open)+len(c.Close) && strings.HasPrefix(t, c.Open) && strings.HasSuffix(t, c.Close)
}

func uncommentLine(line string, c Comment) string {
	body := strings.TrimLeft(line, " \t")
	pad := line[:len(line)-len(body)]
	if c.Line != "" {
		body = strings.TrimPrefix(strings.TrimPrefix(body, c.Line), " ")
		return pad + body
	}
	body = strings.TrimRight(body, " \t")
	body = strings.TrimSuffix(strings.TrimPrefix(body, c.Open), c.Close)
	body = strings.TrimSuffix(strings.TrimPrefix(body, " "), " ")
	return pad + body
}

// commonIndent is the longest leading whitespace shared by the lines at idx.
func commonIndent(lines []string, idx []int) string {
	var pad string
	for n, i := range idx {
		l := lines[i]
		ws := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if n == 0 {
			pad = ws
			continue
		}
		k := 0
		for k < len(pad) && k < len(ws) && pad[k] == ws[k] {
			k++
		}
		pad = pad[:k]
	}
	return pad
}

// toggleComment applies ToggleComment to buffer lines lo..hi as one undo
// step and reports it on the status line. The cursor lands on the first
// line of the range.
func (e *Editor) toggleComment(lo, hi int) {
	c, ok := CommentFor(e.path)
	if !ok {
		e.message = "no comment syntax for this file type"
		return
	}
	lo, hi = max(lo, 0), min(hi, len(e.buf.lines)-1)
	if lo > hi {
		return
	}
	out, act := ToggleComment(e.buf.lines[lo:hi+1], c)
	if act == CommentNone {
		return
	}
	e.buf.snapshotBefore()
	copy(e.buf.lines[lo:hi+1], out)
	e.buf.dirty = true
	e.buf.seq++
	e.buf.SetCursor(Pos{Line: lo, Col: firstNonSpaceRune(e.buf.lines[lo])})
	n := hi - lo + 1
	word := "lines"
	if n == 1 {
		word = "line"
	}
	verb := "commented"
	if act == CommentRemoved {
		verb = "uncommented"
	}
	e.message = itoa(n) + " " + word + " " + verb
}

// toggleCommentHere is ctrl+/: the current line (count lines in normal
// mode). The cursor keeps its place in the text, so typing in insert mode
// carries on where it was.
func (e *Editor) toggleCommentHere(count int) {
	cur := e.buf.Cursor()
	before := len([]rune(e.buf.lines[cur.Line]))
	e.toggleComment(cur.Line, cur.Line+count-1)
	if after := len([]rune(e.buf.lines[cur.Line])); count == 1 && after != before {
		e.buf.SetCursor(Pos{Line: cur.Line, Col: max(cur.Col+after-before, 0)})
	}
}

// commentLines maps an operator range to the lines it touches: a range
// ending at column 0 of a later line (every linewise one) stops above it.
func commentLines(a, z Pos) (int, int) {
	hi := z.Line
	if z.Col == 0 && z.Line > a.Line {
		hi--
	}
	return a.Line, hi
}
