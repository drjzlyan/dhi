package clirun

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The OS sandboxes refuse a write with EPERM ("Operation not permitted");
// a plain Unix permission problem is EACCES ("Permission denied"), which
// no sandbox grant would fix, so it is not matched.
var (
	epermRe  = regexp.MustCompile(`(?i)\bEPERM\b|operation not permitted`)
	quotedRe = regexp.MustCompile(`['"](/[^'"]+)['"]`)
	bareRe   = regexp.MustCompile(`(?:^|[\s:(])(/[^\s:'"()]+)`)
	mkdirRe  = regexp.MustCompile(`(?i)\bmkdir\b`)
)

// DeniedPath extracts the directory a sandbox write denial names, for the
// access card (F-087): the path itself for a mkdir, else its parent. ""
// when text is not an EPERM denial or names no absolute path.
func DeniedPath(text string) string {
	if !epermRe.MatchString(text) {
		return ""
	}
	p := ""
	if m := quotedRe.FindStringSubmatch(text); m != nil {
		p = m[1]
	} else if m := bareRe.FindStringSubmatch(text); m != nil {
		p = m[1]
	}
	if p == "" {
		return ""
	}
	p = filepath.Clean(strings.TrimRight(p, ".,;"))
	if !mkdirRe.MatchString(text) {
		p = filepath.Dir(p)
	}
	if p == "/" {
		return ""
	}
	return p
}
