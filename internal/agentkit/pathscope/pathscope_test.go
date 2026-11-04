package pathscope

import (
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		glob, file string
		want       bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"src/**", "src/a/b.ts", true},
		{"src/**", "lib/a.ts", false},
		{"src/**/*.ts", "src/x.ts", true},
		{"src/**/*.ts", "src/a/b/x.ts", true},
		{"src/*.ts", "src/a/x.ts", false},
		{"*.md", "docs/readme.md", true},
		{"docs/*.md", "other/docs/a.md", false},
		{"internal/tui/**", "internal/tui/app/a.go", true},
		{"", "a.go", false},
	} {
		if got := Match(c.glob, c.file); got != c.want {
			t.Errorf("Match(%q,%q) = %v, want %v", c.glob, c.file, got, c.want)
		}
	}
}

func TestAnyStripsMember(t *testing.T) {
	if !Any([]string{"src/**/*.go"}, []string{"api/src/x/y.go"}) {
		t.Error("member-prefixed path should match a repo-relative glob")
	}
	if Any([]string{"src/**"}, nil) {
		t.Error("no files, no match")
	}
}

func TestFiles(t *testing.T) {
	got := Files("fix `api/src/x.go` and @web/app.ts, see https://x.dev/a.html. also README and b/c.go:12")
	want := []string{"api/src/x.go", "b/c.go", "web/app.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
}
