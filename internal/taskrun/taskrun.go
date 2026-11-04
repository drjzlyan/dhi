// Package taskrun lists the tasks a project already declares in its own
// files (package.json scripts, Makefile targets, justfile recipes) and
// turns a finished task's output into problems (F-146). It invents no file
// format (ADR-0043) and runs nothing itself: callers run the Argv in a
// terminal tab.
package taskrun

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Task is one runnable entry from a project's task file.
type Task struct {
	Name   string
	Runner string   // npm, pnpm, yarn, make, just
	Dir    string   // where it runs
	Argv   []string // Argv[0] is the runner's name; the caller resolves it on the user's PATH
	Hint   string   // the script's command or the recipe's comment
}

// Command is the line the user would type.
func (t Task) Command() string { return strings.Join(t.Argv, " ") }

// JustList runs `just --list --unsorted` in dir and returns its output.
type JustList func(dir string) (string, error)

// Discover lists the tasks in dir. justList is the host's `just`; nil
// means just is not on the user's PATH, and the justfile is read directly.
// A file that exists but cannot be read is reported in notes, not skipped.
func Discover(dir string, justList JustList) (tasks []Task, notes []string) {
	if ts, err := packageScripts(dir); err != nil {
		notes = append(notes, "package.json: "+err.Error())
	} else {
		tasks = append(tasks, ts...)
	}
	tasks = append(tasks, makeTargets(dir)...)
	ts, err := justRecipes(dir, justList)
	if err != nil {
		notes = append(notes, "justfile: "+err.Error())
	}
	return append(tasks, ts...), notes
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// --- package.json ---

// packageManager picks the runner from the lockfile.
func packageManager(dir string) string {
	switch {
	case exists(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm"
	case exists(filepath.Join(dir, "yarn.lock")):
		return "yarn"
	}
	return "npm"
}

type taskErr string

func (e taskErr) Error() string { return string(e) }

const errBad = taskErr("not a JSON object with string scripts")

// packageScripts reads "scripts" in file order.
func packageScripts(dir string) ([]Task, error) {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, nil // no package.json is not an error
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errBad
	}
	pm := packageManager(dir)
	var out []Task
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if k != "scripts" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
			continue
		}
		if t, err := dec.Token(); err != nil || t != json.Delim('{') {
			return nil, errBad
		}
		for dec.More() {
			name, err := dec.Token()
			if err != nil {
				return nil, err
			}
			var cmd string
			if err := dec.Decode(&cmd); err != nil {
				return nil, err
			}
			n := name.(string)
			out = append(out, Task{Name: n, Runner: pm, Dir: dir, Argv: []string{pm, "run", n}, Hint: cmd})
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// --- Makefile ---

var makeRule = regexp.MustCompile(`^([A-Za-z0-9_][^\s:=#$%]*(?:[ \t]+[A-Za-z0-9_][^\s:=#$%]*)*)[ \t]*:([^=]|$)(.*)$`)

// makeTargets reads simple `target:` lines. Pattern rules, special
// targets (.PHONY), variable assignments and indented recipe lines are
// skipped; `## text` after the colon becomes the hint.
func makeTargets(dir string) []Task {
	var f *os.File
	for _, n := range []string{"GNUmakefile", "makefile", "Makefile"} {
		if g, err := os.Open(filepath.Join(dir, n)); err == nil {
			f = g
			break
		}
	}
	if f == nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []Task
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		m := makeRule.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		hint := ""
		if i := strings.Index(m[3], "##"); i >= 0 {
			hint = strings.TrimSpace(m[3][i+2:])
		}
		for _, name := range strings.Fields(m[1]) {
			if strings.ContainsAny(name, "*?[") || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Task{Name: name, Runner: "make", Dir: dir, Argv: []string{"make", name}, Hint: hint})
		}
	}
	return out
}

// --- justfile ---

var justRecipe = regexp.MustCompile(`^@?([A-Za-z_][A-Za-z0-9_-]*)((?:[ \t]+[^:=\s]\S*)*)[ \t]*:([^=]|$)`)

func justRecipes(dir string, list JustList) ([]Task, error) {
	var file string
	for _, n := range []string{"justfile", "Justfile", ".justfile"} {
		if exists(filepath.Join(dir, n)) {
			file = filepath.Join(dir, n)
			break
		}
	}
	if file == "" {
		return nil, nil
	}
	mk := func(name, hint string) Task {
		return Task{Name: name, Runner: "just", Dir: dir, Argv: []string{"just", name}, Hint: hint}
	}
	var out []Task
	if list != nil {
		txt, err := list(dir)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(txt, "\n") {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				continue // "Available recipes:" and group headings
			}
			t := strings.TrimSpace(line)
			hint := ""
			if i := strings.Index(t, "#"); i >= 0 {
				hint, t = strings.TrimSpace(t[i+1:]), strings.TrimSpace(t[:i])
			}
			if f := strings.Fields(t); len(f) > 0 && !strings.HasPrefix(f[0], "[") {
				out = append(out, mk(f[0], hint))
			}
		}
		return out, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	comment := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "#") {
			comment = strings.TrimSpace(strings.TrimLeft(line, "#"))
			continue
		}
		if m := justRecipe.FindStringSubmatch(line); m != nil {
			out = append(out, mk(m[1], comment))
		}
		comment = ""
	}
	return out, nil
}

// --- problem matchers ---

// Problem is one file:line:col message found in a task's output.
type Problem struct {
	Path     string // absolute
	Line     int    // 1-based
	Col      int    // 1-based, 0 when unknown
	Severity int    // 1 error, 2 warning, 3 info (LSP numbering)
	Message  string
}

var (
	// unixLine matches Go, gcc/clang and eslint -f unix output:
	// `path:line[:col]: [severity:] message`.
	unixLine = regexp.MustCompile(`^\s*([^\s:()]+\.[A-Za-z0-9]+):(\d+)(?::(\d+))?:\s*(.*\S)\s*$`)
	// tscLine matches tsc's `path(line,col): error TS1234: message`.
	tscLine = regexp.MustCompile(`^\s*([^\s:()]+\.[A-Za-z0-9]+)\((\d+),(\d+)\):\s*(.*\S)\s*$`)
	sevWord = regexp.MustCompile(`^(?i:fatal error|error|warning|note|info)\b:?\s*`)
)

const maxProblems = 500

// ParseProblems extracts problems from a finished task's output. Paths
// resolve against dir; lines naming files that do not exist are dropped,
// which keeps timestamps and URLs out.
func ParseProblems(output, dir string) []Problem {
	var out []Problem
	seen := map[Problem]bool{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		var file, ln, col, msg string
		if m := unixLine.FindStringSubmatch(line); m != nil {
			file, ln, col, msg = m[1], m[2], m[3], m[4]
		} else if m := tscLine.FindStringSubmatch(line); m != nil {
			file, ln, col, msg = m[1], m[2], m[3], m[4]
		} else {
			continue
		}
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if fi, err := os.Stat(path); err != nil || fi.IsDir() {
			continue
		}
		p := Problem{Path: path, Line: atoi(ln), Col: atoi(col), Severity: 1, Message: msg}
		if w := sevWord.FindString(msg); w != "" {
			switch strings.ToLower(strings.TrimRight(strings.TrimSpace(w), ":")) {
			case "warning":
				p.Severity = 2
			case "note", "info":
				p.Severity = 3
			}
			p.Message = strings.TrimSpace(msg[len(w):])
		}
		if p.Line < 1 || p.Message == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) == maxProblems {
			break
		}
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
