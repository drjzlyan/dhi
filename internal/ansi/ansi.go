// Package ansi provides terminal escape-sequence utilities shared by
// rendering and testing code.
package ansi

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var re = regexp.MustCompile(`\x1b\[[0-9;:?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-B]`)

// Strip removes ANSI escape sequences, returning visible text only.
func Strip(s string) string { return re.ReplaceAllString(s, "") }

// Clip returns the longest prefix of s whose visible width is at most n
// cells, keeping escape sequences intact so styling survives truncation.
func Clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				b.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		if width >= n {
			break
		}
		b.WriteRune(r)
		width++
		i += sz
	}
	return b.String()
}
