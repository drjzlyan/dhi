// Package ansi provides terminal escape-sequence utilities shared by
// rendering and testing code.
package ansi

import "regexp"

var re = regexp.MustCompile(`\x1b\[[0-9;:?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-B]`)

// Strip removes ANSI escape sequences, returning visible text only.
func Strip(s string) string { return re.ReplaceAllString(s, "") }
