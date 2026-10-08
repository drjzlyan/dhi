// Package tutorial holds DHI's guided lessons (F-048): short declarative
// walkthroughs the shell plays as a coach strip over the live UI. A step may
// wait for something the shell can observe (a view switch, the palette, the
// help overlay); every other step is read, tried and continued by the user.
// The package is pure: it knows nothing about rendering.
package tutorial

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed lessons/*.toml
var lessonsFS embed.FS

// Step is one instruction.
type Step struct {
	Title string
	Body  string
	// Await is the observable event that completes the step: "" (the user
	// continues manually), "palette", "help", "view:<surface id>" or
	// "do:<action event>" — a real action a surface reports (events.go).
	Await string
}

// Tutorial is one lesson.
type Tutorial struct {
	Slug        string
	Name        string
	Description string
	Steps       []Step
}

type file struct {
	Schema      int    `toml:"schema"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Step        []struct {
		Title string `toml:"title"`
		Body  string `toml:"body"`
		Await string `toml:"await"`
	} `toml:"step"`
}

// Surfaces a "view:" await may name — the shell's registered view ids.
var viewIDs = map[string]bool{"workspace": true, "editor": true, "ideator": true, "reviewer": true, "settings": true}

// ValidAwait reports whether an await string is in the grammar.
func ValidAwait(a string) bool {
	switch {
	case a == "", a == "palette", a == "help":
		return true
	case strings.HasPrefix(a, "view:"):
		return viewIDs[strings.TrimPrefix(a, "view:")]
	case strings.HasPrefix(a, "do:"):
		_, ok := Events[strings.TrimPrefix(a, "do:")]
		return ok
	}
	return false
}

func parse(slug string, data []byte) (Tutorial, error) {
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Tutorial{}, err
	}
	if und := md.Undecoded(); len(und) > 0 {
		var keys []string
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return Tutorial{}, fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
	}
	if f.Schema != 1 {
		return Tutorial{}, fmt.Errorf("schema %d, want 1", f.Schema)
	}
	t := Tutorial{Slug: slug, Name: strings.TrimSpace(f.Name), Description: strings.TrimSpace(f.Description)}
	if t.Name == "" || t.Description == "" {
		return Tutorial{}, fmt.Errorf("name and description are required")
	}
	for i, s := range f.Step {
		st := Step{Title: strings.TrimSpace(s.Title), Body: strings.TrimSpace(s.Body), Await: strings.TrimSpace(s.Await)}
		if st.Title == "" || st.Body == "" {
			return Tutorial{}, fmt.Errorf("step %d needs a title and a body", i+1)
		}
		if !ValidAwait(st.Await) {
			return Tutorial{}, fmt.Errorf("step %d: unknown await %q", i+1, st.Await)
		}
		t.Steps = append(t.Steps, st)
	}
	if len(t.Steps) == 0 {
		return Tutorial{}, fmt.Errorf("a tutorial needs at least one step")
	}
	return t, nil
}

// order is the teaching order; unlisted lessons follow alphabetically.
var order = []string{"tour", "team", "editor", "review", "debug"}

// All lists every lesson in teaching order.
func All() []Tutorial {
	entries, err := lessonsFS.ReadDir("lessons")
	if err != nil {
		return nil
	}
	byslug := map[string]Tutorial{}
	for _, e := range entries {
		data, err := lessonsFS.ReadFile("lessons/" + e.Name())
		if err != nil {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".toml")
		if t, err := parse(slug, data); err == nil {
			byslug[slug] = t
		}
	}
	var out []Tutorial
	for _, s := range order {
		if t, ok := byslug[s]; ok {
			out = append(out, t)
			delete(byslug, s)
		}
	}
	var rest []string
	for s := range byslug {
		rest = append(rest, s)
	}
	sort.Strings(rest)
	for _, s := range rest {
		out = append(out, byslug[s])
	}
	return out
}

// Get returns one lesson.
func Get(slug string) (Tutorial, bool) {
	for _, t := range All() {
		if t.Slug == slug {
			return t, true
		}
	}
	return Tutorial{}, false
}
