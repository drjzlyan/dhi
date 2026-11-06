// Package vscsnippets reads VS Code snippet files (F-171, ADR-0043: a
// standard format, no DHI one): <repo>/.vscode/*.code-snippets, JSON with
// comments, each entry {prefix, body, description, scope}.
package vscsnippets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Snippet is one prefix of one entry.
type Snippet struct {
	Name   string
	Prefix string
	Body   string // lines joined with \n; snippet syntax
	Desc   string
	Scope  []string // language ids; empty = every language
}

// Matches reports whether the snippet applies to a file of language id
// lang ("" = unknown language: only unscoped snippets).
func (s Snippet) Matches(lang string) bool {
	if len(s.Scope) == 0 {
		return true
	}
	for _, sc := range s.Scope {
		if lang != "" && sc == lang {
			return true
		}
	}
	return false
}

type entry struct {
	Prefix      json.RawMessage `json:"prefix"`
	Body        json.RawMessage `json:"body"`
	Description string          `json:"description"`
	Scope       string          `json:"scope"`
}

// Dir loads every .code-snippets file in <root>/.vscode, plus a signature
// of what was read (names, sizes, mtimes) so callers can cache. A file
// that fails to parse is reported in errs and skipped; the others load.
func Dir(root string) (snips []Snippet, sig string, errs []error) {
	files, _ := filepath.Glob(filepath.Join(root, ".vscode", "*.code-snippets"))
	sort.Strings(files)
	var sb strings.Builder
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "%s|%d|%d;", f, st.Size(), st.ModTime().UnixNano())
		data, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		got, err := Parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(f), err))
			continue
		}
		snips = append(snips, got...)
	}
	return snips, sb.String(), errs
}

// Parse reads one snippet file (JSON with comments and trailing commas).
func Parse(data []byte) ([]Snippet, error) {
	var file map[string]entry
	if err := json.Unmarshal(stripJSONC(data), &file); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(file))
	for n := range file {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Snippet
	for _, n := range names {
		e := file[n]
		prefixes, err := stringsOrOne(e.Prefix)
		if err != nil {
			return nil, fmt.Errorf("%q prefix: %w", n, err)
		}
		body, err := stringsOrOne(e.Body)
		if err != nil {
			return nil, fmt.Errorf("%q body: %w", n, err)
		}
		var scope []string
		for _, sc := range strings.Split(e.Scope, ",") {
			if sc = strings.TrimSpace(sc); sc != "" {
				scope = append(scope, sc)
			}
		}
		for _, p := range prefixes {
			out = append(out, Snippet{Name: n, Prefix: p, Body: strings.Join(body, "\n"), Desc: e.Description, Scope: scope})
		}
	}
	return out, nil
}

func stringsOrOne(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, errors.New("want a string or a list of strings")
	}
	return many, nil
}

// stripJSONC removes // and /* */ comments and trailing commas outside
// strings.
func stripJSONC(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch {
		case c == '"':
			out = append(out, c)
			for i++; i < len(in); i++ {
				out = append(out, in[i])
				if in[i] == '\\' && i+1 < len(in) {
					i++
					out = append(out, in[i])
				} else if in[i] == '"' {
					break
				}
			}
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			i += 2
			for i+1 < len(in) && (in[i] != '*' || in[i+1] != '/') {
				i++
			}
			i++
		case c == ',':
			j := i + 1
			for j < len(in) && (in[j] == ' ' || in[j] == '\t' || in[j] == '\n' || in[j] == '\r') {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
