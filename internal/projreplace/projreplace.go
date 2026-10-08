// Package projreplace plans and applies a project-wide replace over the
// lines a search found (F-056). It is pure: Plan turns hits into a change
// list, Apply writes it through a Files seam (open buffers or disk), and
// neither knows about the TUI.
package projreplace

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/drjzlyan/dhi/internal/search"
)

// Change is one line's replacement.
type Change struct {
	Path string // absolute file path
	Line int    // 1-based
	Old  string // the line as the search saw it
	New  string
}

// Compile builds the matcher the search used: pattern quoted unless
// regex, case-insensitive when it has no upper-case letter (rg's smart
// case, which DHI's search passes as -S).
func Compile(pattern string, regex bool) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	expr := pattern
	if !regex {
		expr = regexp.QuoteMeta(pattern)
	}
	if !strings.ContainsFunc(pattern, unicode.IsUpper) {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	return re, nil
}

// Plan computes the replacement for every hit line, in path then line
// order. Lines the pattern does not change are left out. In fixed-string
// mode the replacement is literal; in regex mode it may use $1 / ${name}.
func Plan(hits []search.Hit, pattern string, regex bool, repl string) ([]Change, error) {
	re, err := Compile(pattern, regex)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Change
	for _, h := range hits {
		key := fmt.Sprintf("%s:%d", h.Path, h.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		var next string
		if regex {
			next = re.ReplaceAllString(h.Text, repl)
		} else {
			next = re.ReplaceAllLiteralString(h.Text, repl)
		}
		if next != h.Text {
			out = append(out, Change{Path: h.Path, Line: h.Line, Old: h.Text, New: next})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// Files reads and writes whole files by line: the editor implements it
// over open buffers (one undo step each) and the disk.
type Files interface {
	Lines(path string) ([]string, error)
	SetLines(path string, edits map[int]string) error // 0-based line → text
}

// Result reports what Apply did.
type Result struct {
	Files int      // files written
	Lines int      // lines replaced
	Stale []Change // lines that changed since the search; skipped
	Errs  []error  // per-file read/write failures
}

// Apply writes changes file by file. A line whose current text differs
// from Change.Old is stale (edited since the search) and is skipped,
// never clobbered.
func Apply(changes []Change, files Files) Result {
	var res Result
	byPath := map[string][]Change{}
	var order []string
	for _, c := range changes {
		if _, ok := byPath[c.Path]; !ok {
			order = append(order, c.Path)
		}
		byPath[c.Path] = append(byPath[c.Path], c)
	}
	for _, path := range order {
		lines, err := files.Lines(path)
		if err != nil {
			res.Errs = append(res.Errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		edits := map[int]string{}
		for _, c := range byPath[path] {
			i := c.Line - 1
			if i < 0 || i >= len(lines) || lines[i] != c.Old {
				res.Stale = append(res.Stale, c)
				continue
			}
			edits[i] = c.New
		}
		if len(edits) == 0 {
			continue
		}
		if err := files.SetLines(path, edits); err != nil {
			res.Errs = append(res.Errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		res.Files++
		res.Lines += len(edits)
	}
	return res
}

// Summary is the one-line outcome for a flash.
func (r Result) Summary() string {
	s := fmt.Sprintf("replaced %d line(s) in %d file(s)", r.Lines, r.Files)
	if n := len(r.Stale); n > 0 {
		s += fmt.Sprintf(" · %d skipped (changed since the search)", n)
	}
	if n := len(r.Errs); n > 0 {
		s += fmt.Sprintf(" · %d file(s) failed: %v", n, r.Errs[0])
	}
	return s
}
