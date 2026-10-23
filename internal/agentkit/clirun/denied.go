package clirun

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The OS sandboxes refuse a write with EPERM ("Operation not permitted";
// bubblewrap's read-only binds give EROFS, "Read-only file system"); a
// plain Unix permission problem is EACCES ("Permission denied"), which no
// sandbox grant would fix, so it is not matched.
var (
	epermRe  = regexp.MustCompile(`(?i)\bEPERM\b|operation not permitted|\bEROFS\b|read-only file system`)
	quotedRe = regexp.MustCompile(`['"](/[^'"]+)['"]`)
	bareRe   = regexp.MustCompile(`(?:^|[\s:(])(/[^\s:'"()]+)`)
	mkdirRe  = regexp.MustCompile(`(?i)\bmkdir\b`)
	writeRe  = regexp.MustCompile(`(?i)\b(mkdir|open|write|touch|create|rename|unlink|cp|mv)\b`)
)

// homeDir is swapped by tests.
var homeDir = os.UserHomeDir

// DeniedPath extracts what a sandbox denial names, for the access card
// (F-087, F-113): for a write, the directory (the path itself for a
// mkdir; a file directly in your home, like ~/.zshrc, stays a file so the
// grant is that file, never your whole home); for a program the sandbox
// would not run, the program itself (the card asks to run programs from
// its folder). "" when text is not a denial or names no absolute path.
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
	if p == "/" || p == "/dev" || strings.HasPrefix(p, "/dev/") {
		return "" // device nodes are the sandbox's business, never a grant
	}
	if strings.Contains(p+"/", "/.git/") {
		return "" // git internals: the default branch is locked on purpose (work_branch)
	}
	if IsProgram(p) && !writeRe.MatchString(text) {
		return p // an exec denial: the card offers to run programs from its folder
	}
	if mkdirRe.MatchString(text) {
		return p
	}
	if home, err := homeDir(); err == nil && home != "" && filepath.Dir(p) == filepath.Clean(home) {
		return p // a file in your home: grant the file, not ~
	}
	p = filepath.Dir(p)
	if p == "/" {
		return ""
	}
	return p
}

// IsProgram reports an existing executable regular file.
func IsProgram(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}
