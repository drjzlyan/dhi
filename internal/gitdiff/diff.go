// Package gitdiff parses unified diffs in git format into a GitHub-style
// file/hunk/line model shared by unified and side-by-side rendering, and
// pairs hunk lines into aligned rows for the two-column layout.
//
// Parse is a pure function of patch text: it never invokes git. Callers
// obtain patches from the hermetic CLI (`git diff --no-color base...head`)
// or from external sources such as `gh pr diff`.
package gitdiff

import (
	"strconv"
	"strings"
)

// Kind classifies a diff line's relationship to the two sides.
type Kind uint8

const (
	Ctx Kind = iota // present on both sides
	Add             // only on the new side
	Del             // only on the old side
)

// Line is one row of a hunk body. OldNo/NewNo are 1-based file line
// numbers on their respective sides; the absent side carries 0.
type Line struct {
	Kind  Kind
	OldNo int
	NewNo int
	Text  string // content without the sign prefix
}

// Hunk is a single @@-delimited block. Start values come from the header;
// Lines are numbered from there.
type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Header   string // optional trailing section heading after the second @@
	Lines    []Line
}

// FileDiff describes changes to exactly one path pair. Mode/new/deleted/
// rename/binary metadata mirrors git's extended headers so the UI can
// badge files without re-inspecting content.
type FileDiff struct {
	OldPath   string
	NewPath   string
	OldMode   string
	NewMode   string
	IsNew     bool
	IsDeleted bool
	IsRename  bool
	IsBinary  bool
	Hunks     []Hunk
}

// DisplayPath picks the surviving path for listings (new side, falling
// back to old for deletions).
func (f FileDiff) DisplayPath() string {
	if f.NewPath != "" {
		return f.NewPath
	}
	return f.OldPath
}

// Stat counts added and removed lines across all hunks.
func (f FileDiff) Stat() (adds, dels int) {
	for _, h := range f.Hunks {
		a, d := h.Stat()
		adds += a
		dels += d
	}
	return adds, dels
}

// Stat counts added and removed lines in the hunk.
func (h Hunk) Stat() (adds, dels int) {
	for _, l := range h.Lines {
		switch l.Kind {
		case Add:
			adds++
		case Del:
			dels++
		}
	}
	return adds, dels
}

// Parse splits a whole patch stream into per-file diffs. Content outside
// `diff --git` sections (format-patch prologs, commit messages) is skipped.
// Malformed fragments degrade: unknown lines terminate the current hunk,
// never the parse.
func Parse(patch string) []FileDiff {
	var files []FileDiff
	var cur *FileDiff
	var hunk *Hunk
	nextOld, nextNew := 0, 0

	flushHunk := func() {
		if hunk != nil && cur != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
		}
		hunk = nil
	}
	flushFile := func() {
		flushHunk()
		cur = nil
	}

	lines := strings.Split(patch, "\n")
	for i, raw := range lines {
		if i == len(lines)-1 && raw == "" {
			continue // artifact of a trailing newline, not content
		}
		line := strings.TrimSuffix(raw, "\r")
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			oldP, newP := headerPaths(line)
			files = append(files, FileDiff{OldPath: oldP, NewPath: newP})
			cur = &files[len(files)-1]

		case cur == nil:
			continue // preamble before the first file section

		case strings.HasPrefix(line, "@@ "):
			// Hunk boundaries are handled ahead of body state: a header
			// may follow content directly (adjacent hunks are the norm).
			flushHunk()
			if h := parseHunkHeader(line); h != nil {
				nextOld, nextNew = h.OldStart, h.NewStart
				hunk = h
			}

		case hunk == nil:
			switch {
			case strings.HasPrefix(line, "old mode "):
				cur.OldMode = field(line)
			case strings.HasPrefix(line, "new mode "):
				cur.NewMode = field(line)
			case strings.HasPrefix(line, "new file mode "):
				cur.IsNew = true
				cur.NewMode = field(line)
			case strings.HasPrefix(line, "deleted file mode "):
				cur.IsDeleted = true
				cur.OldMode = field(line)
			case strings.HasPrefix(line, "rename from "):
				cur.IsRename = true
				if p, ok := unquote(strings.TrimPrefix(line, "rename from ")); ok {
					cur.OldPath = p
				}
			case strings.HasPrefix(line, "rename to "):
				cur.IsRename = true
				if p, ok := unquote(strings.TrimPrefix(line, "rename to ")); ok {
					cur.NewPath = p
				}
			case strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch":
				cur.IsBinary = true
			case strings.HasPrefix(line, "--- "):
				if p, ok := diffPath(strings.TrimPrefix(line, "--- ")); ok {
					cur.OldPath = p
				}
			case strings.HasPrefix(line, "+++ "):
				if p, ok := diffPath(strings.TrimPrefix(line, "+++ ")); ok {
					cur.NewPath = p
				}
			}

		default: // inside a hunk body
			body := line
			var kind Kind
			switch {
			case strings.HasPrefix(body, "\\"):
				continue // "\ No newline at end of file" — not content
			case strings.HasPrefix(body, " "):
				kind = Ctx
				body = strings.TrimPrefix(body, " ")
			case body == "":
				kind = Ctx // empty context line lost its space to mailer trim
			case strings.HasPrefix(body, "-"):
				kind = Del
				body = strings.TrimPrefix(body, "-")
			case strings.HasPrefix(body, "+"):
				kind = Add
				body = strings.TrimPrefix(body, "+")
			default:
				flushHunk() // unknown line ends the hunk; parsing continues
				continue
			}
			l := Line{Kind: kind, Text: body}
			switch kind {
			case Ctx:
				l.OldNo, l.NewNo = nextOld, nextNew
				nextOld++
				nextNew++
			case Del:
				l.OldNo = nextOld
				nextOld++
			case Add:
				l.NewNo = nextNew
				nextNew++
			}
			hunk.Lines = append(hunk.Lines, l)
		}
	}
	flushFile()
	return files
}

// headerPaths extracts both paths from a `diff --git a/x b/y` line,
// honoring git's C-quoting of exotic names.
func headerPaths(line string) (string, string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	if q1 := strings.IndexByte(rest, '"'); q1 >= 0 {
		p1, n := scanQuoted(rest[q1:])
		p2 := ""
		if tail := strings.TrimSpace(rest[q1+n:]); strings.HasPrefix(tail, "\"") || !strings.Contains(tail, " ") {
			p2, _ = unquote(tail)
		} else {
			p2, _ = unquote(afterAB(tail))
		}
		return p1, p2
	}
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return "", ""
	}
	return stripAB(fields[0]), stripAB(fields[len(fields)-1])
}

// afterAB skips a leading a/ or b/ token boundary inside an already-split
// tail (used when the first path was quoted).
func afterAB(s string) string {
	if strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	return s
}

// scanQuoted reads one C-quoted string from the front of s, returning its
// unquoted value and consumed byte count.
func scanQuoted(s string) (string, int) {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			v, _ := unquote(s[:i+1])
			return v, i + 1
		}
	}
	v, _ := unquote(s)
	return v, len(s)
}

// unquote decodes a git C-quoted token; plain tokens pass through.
func unquote(s string) (string, bool) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if v, err := strconv.Unquote(s); err == nil {
			return v, true
		}
	}
	return s, false
}

// stripAB removes a leading a/ or b/ prefix.
func stripAB(p string) string {
	if len(p) > 2 && p[0] != '"' && p[1] == '/' && (p[0] == 'a' || p[0] == 'b') {
		return p[2:]
	}
	return p
}

// diffPath parses a ---/+++ argument: /dev/null means "no file".
func diffPath(tok string) (string, bool) {
	tok = strings.TrimSpace(tok)
	if i := strings.Index(tok, "\t"); i >= 0 {
		tok = tok[:i]
	}
	tok = strings.TrimSuffix(tok, "\r")
	if tok == "/dev/null" {
		return "", true
	}
	if v, ok := unquote(tok); ok {
		return stripAB(v), true
	}
	return stripAB(tok), true
}

// field returns the last whitespace-separated token (mode values).
func field(line string) string {
	if i := strings.LastIndexByte(line, ' '); i >= 0 {
		return line[i+1:]
	}
	return ""
}

// parseHunkHeader decodes `@@ -l[,s] +l[,s] @@ [heading]`.
func parseHunkHeader(line string) *Hunk {
	rest := strings.TrimPrefix(line, "@@ -")
	dash := strings.IndexByte(rest, ' ')
	if dash < 0 {
		return nil
	}
	oldPart := rest[:dash]
	plus := strings.IndexByte(rest, '+')
	if plus < 0 {
		return nil
	}
	tail := rest[plus+1:]
	at := strings.Index(tail, "@@")
	if at < 0 {
		return nil
	}
	newPart := strings.TrimSpace(tail[:at])
	header := strings.TrimSpace(tail[at+2:])

	oStart, oLen, ok := range2(strings.TrimSpace(oldPart))
	if !ok {
		return nil
	}
	nStart, nLen, ok := range2(newPart)
	if !ok {
		return nil
	}
	return &Hunk{OldStart: oStart, OldLines: oLen, NewStart: nStart, NewLines: nLen, Header: header}
}

// range2 parses "<start>[,<count>]", count defaulting to 1.
func range2(s string) (start, count int, ok bool) {
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		n, err := strconv.Atoi(s)
		return n, 1, err == nil
	}
	start, err := strconv.Atoi(s[:comma])
	if err != nil {
		return 0, 0, false
	}
	count, err = strconv.Atoi(s[comma+1:])
	if err != nil {
		return 0, 0, false
	}
	return start, count, true
}
