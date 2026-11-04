// Package pathscope decides whether a path-scoped rule applies to a turn
// (F-152). Rules and skills carry a `paths:` list of globs, the field
// Claude Code uses for path-scoped rules; a scoped item enters the prompt
// only when the turn concerns a file one of its globs matches.
package pathscope

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Match reports whether file (slash-separated, workspace-relative)
// matches glob. `**` spans any number of directories (including none),
// `*`, `?` and `[...]` follow path.Match within one segment. A glob with
// no slash matches the base name at any depth, as in .gitignore.
func Match(glob, file string) bool {
	glob = strings.TrimPrefix(strings.TrimSpace(glob), "./")
	file = strings.TrimPrefix(file, "./")
	if glob == "" || file == "" {
		return false
	}
	if !strings.Contains(glob, "/") {
		glob = "**/" + glob
	}
	return matchSegs(strings.Split(glob, "/"), strings.Split(file, "/"))
}

func matchSegs(g, f []string) bool {
	for len(g) > 0 {
		if g[0] == "**" {
			for len(g) > 1 && g[1] == "**" {
				g = g[1:]
			}
			if len(g) == 1 {
				return true
			}
			for i := 0; i <= len(f); i++ {
				if matchSegs(g[1:], f[i:]) {
					return true
				}
			}
			return false
		}
		if len(f) == 0 {
			return false
		}
		if ok, err := path.Match(g[0], f[0]); err != nil || !ok {
			return false
		}
		g, f = g[1:], f[1:]
	}
	return len(f) == 0
}

// Any reports whether some glob matches some file. Files may carry a
// leading member name ("api/src/x.go"); a glob is also tried against the
// path without its first segment, since rules are written relative to a repo.
func Any(globs, files []string) bool {
	for _, f := range files {
		for _, g := range globs {
			if Match(g, f) {
				return true
			}
			if _, rest, ok := strings.Cut(f, "/"); ok && Match(g, rest) {
				return true
			}
		}
	}
	return false
}

var (
	tokenRe = regexp.MustCompile("[^\\s`'\"(),;<>\\[\\]{}]+")
	lineRe  = regexp.MustCompile(`:\d+(:\d+)?$`)
)

// Files extracts the file paths a message mentions: tokens that look like
// a path (a slash or an extension), with the @ of a mention, a :line
// suffix and trailing punctuation removed. URLs are skipped. The result
// is sorted and free of duplicates.
func Files(text string) []string {
	seen := map[string]bool{}
	for _, tok := range tokenRe.FindAllString(text, -1) {
		tok = strings.TrimPrefix(tok, "@")
		tok = lineRe.ReplaceAllString(strings.TrimRight(tok, ".!?:"), "")
		if tok == "" || strings.Contains(tok, "://") {
			continue
		}
		if !strings.Contains(tok, "/") && !strings.Contains(path.Base(tok), ".") {
			continue
		}
		seen[strings.TrimPrefix(tok, "./")] = true
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
