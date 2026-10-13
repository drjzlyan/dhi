// Package brief is the project brief (F-080): what the project is, who
// it is for, and how the user works. It lives in the tracked
// .dhi/project.toml, is detected from the repo on first run, edited in
// Settings → PROJECT, and rendered into every agent's system prompt.
package brief

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/fsx"
)

// File is the brief's path under the workspace root.
const File = ".dhi/project.toml"

// Brief is the project brief. Every field is free text; multi-line
// values are allowed.
type Brief struct {
	Name    string `toml:"name"`
	Summary string `toml:"summary"`
	Goals   string `toml:"goals"`
	Users   string `toml:"users"`
	Stack   string `toml:"stack"`
	Process string `toml:"process"`
	Review  string `toml:"review"`
	Testing string `toml:"testing"`
	Dont    string `toml:"dont"`
}

// Field names a brief field for forms, proposals and the prompt.
type Field struct {
	Key   string // toml key, also the project_update field name
	Label string // form label (fits 10 columns)
	Title string // prompt heading
}

// Fields lists the brief's fields in display order.
var Fields = []Field{
	{"name", "name", "Project"},
	{"summary", "summary", "What it is"},
	{"goals", "goals", "Goals"},
	{"users", "users", "Who it's for"},
	{"stack", "stack", "Stack"},
	{"process", "process", "How we work"},
	{"review", "review", "Reviews"},
	{"testing", "testing", "Testing"},
	{"dont", "never do", "Never"},
}

func (b *Brief) ptr(key string) *string {
	switch key {
	case "name":
		return &b.Name
	case "summary":
		return &b.Summary
	case "goals":
		return &b.Goals
	case "users":
		return &b.Users
	case "stack":
		return &b.Stack
	case "process":
		return &b.Process
	case "review":
		return &b.Review
	case "testing":
		return &b.Testing
	case "dont":
		return &b.Dont
	}
	return nil
}

// Get returns a field's value ("" for an unknown key).
func (b Brief) Get(key string) string {
	if p := b.ptr(key); p != nil {
		return *p
	}
	return ""
}

// Set changes one field; an unknown key is an error naming the valid ones.
func (b *Brief) Set(key, value string) error {
	p := b.ptr(key)
	if p == nil {
		keys := make([]string, len(Fields))
		for i, f := range Fields {
			keys[i] = f.Key
		}
		return fmt.Errorf("brief: unknown field %q (valid: %s)", key, strings.Join(keys, ", "))
	}
	*p = strings.TrimSpace(value)
	return nil
}

// Empty reports whether nothing is filled in.
func (b Brief) Empty() bool { return b == Brief{} }

type doc struct {
	Schema  int   `toml:"schema"`
	Project Brief `toml:"project"`
}

// Path is the brief's absolute path for root.
func Path(root string) string { return filepath.Join(root, File) }

// Load reads the brief. A missing file returns ok=false and no error; a
// malformed one is an error naming the path (never silently ignored).
func Load(root string) (b Brief, ok bool, err error) {
	data, err := os.ReadFile(Path(root))
	if errors.Is(err, fs.ErrNotExist) {
		return Brief{}, false, nil
	}
	if err != nil {
		return Brief{}, false, err
	}
	var d doc
	md, err := toml.Decode(string(data), &d)
	if err != nil {
		return Brief{}, false, fmt.Errorf("%s: %w", File, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		var keys []string
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return Brief{}, false, fmt.Errorf("%s: unknown key(s): %s", File, strings.Join(keys, ", "))
	}
	if d.Schema != 1 {
		return Brief{}, false, fmt.Errorf("%s: schema %d, want 1", File, d.Schema)
	}
	return d.Project, true, nil
}

// Save writes the brief atomically.
func Save(root string, b Brief) error {
	var buf bytes.Buffer
	buf.WriteString("# Project brief: what this is and how we work. Every DHI employee reads it.\n")
	buf.WriteString("# Edit it in Settings → PROJECT.\n")
	if err := toml.NewEncoder(&buf).Encode(doc{Schema: 1, Project: b}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path(root)), 0o755); err != nil {
		return err
	}
	return fsx.WriteAtomic(Path(root), buf.Bytes(), 0o644)
}

// Render is the brief as a system-prompt block; "" when empty.
func (b Brief) Render() string {
	if b.Empty() {
		return ""
	}
	var s strings.Builder
	s.WriteString("Project brief, written by the person you work for. Treat it as ground truth; " +
		"if the code disagrees with it, say so rather than guessing.\n")
	for _, f := range Fields {
		v := strings.TrimSpace(b.Get(f.Key))
		if v == "" {
			continue
		}
		if strings.Contains(v, "\n") {
			fmt.Fprintf(&s, "%s:\n%s\n", f.Title, indent(v))
		} else {
			fmt.Fprintf(&s, "%s: %s\n", f.Title, v)
		}
	}
	return strings.TrimRight(s.String(), "\n")
}

func indent(v string) string {
	lines := strings.Split(v, "\n")
	for i, l := range lines {
		lines[i] = "  " + strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// InterviewRequest is what the human's message to the lead says when
// they ask to be interviewed about the project (F-080).
const InterviewRequest = "Please interview me about this project so the whole team works the way I do. " +
	"Read the current brief first, then ask me a few short questions at a time: what we are building and for whom, " +
	"the goals, how I like work to flow (tasks, branches, reviews, testing), and anything you must never do. " +
	"When an answer changes the brief, call project_update with the field and the new text; " +
	"I approve each change in Settings → PROPOSALS."
