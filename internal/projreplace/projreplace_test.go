package projreplace

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/search"
)

func hits(lines ...string) []search.Hit {
	var out []search.Hit
	for i, l := range lines {
		path, text, _ := strings.Cut(l, "|")
		out = append(out, search.Hit{Path: path, Line: i + 1, Text: text})
	}
	return out
}

func TestPlanFixedRegexSmartCaseCaptures(t *testing.T) {
	h := hits("/a.go|foo.Bar(foo)", "/a.go|nothing here", "/b.go|FOO and foo")
	got, err := Plan(h, "foo", false, "x.y")
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{Path: "/a.go", Line: 1, Old: "foo.Bar(foo)", New: "x.y.Bar(x.y)"},
		{Path: "/b.go", Line: 3, Old: "FOO and foo", New: "x.y and x.y"}, // smart case: lowercase pattern = any case
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fixed plan =\n%+v\nwant\n%+v", got, want)
	}
	// Upper case in the pattern makes it case-sensitive; "$" stays literal in fixed mode.
	got, _ = Plan(hits("/c.go|Foo foo"), "Foo", false, "$1")
	if len(got) != 1 || got[0].New != "$1 foo" {
		t.Fatalf("case-sensitive literal = %+v", got)
	}
	// Regex with captures.
	got, err = Plan(hits("/d.go|get(user, id)"), `(\w+)\((\w+), (\w+)\)`, true, "$1($3, $2)")
	if err != nil || len(got) != 1 || got[0].New != "get(id, user)" {
		t.Fatalf("regex captures = %+v %v", got, err)
	}
	if _, err := Plan(nil, "(", true, ""); err == nil {
		t.Fatal("invalid regex must be refused")
	}
}

type memFiles struct {
	files map[string][]string
	fail  string
}

func (m *memFiles) Lines(p string) ([]string, error) {
	if l, ok := m.files[p]; ok {
		return append([]string(nil), l...), nil
	}
	return nil, errors.New("missing")
}

func (m *memFiles) SetLines(p string, edits map[int]string) error {
	if p == m.fail {
		return errors.New("read-only")
	}
	for i, t := range edits {
		m.files[p][i] = t
	}
	return nil
}

func TestApplySkipsStaleAndReportsFailures(t *testing.T) {
	fs := &memFiles{files: map[string][]string{
		"/a.go":  {"foo()", "keep", "foo again"},
		"/b.go":  {"edited since"},
		"/ro.go": {"foo"},
	}, fail: "/ro.go"}
	changes := []Change{
		{Path: "/a.go", Line: 1, Old: "foo()", New: "bar()"},
		{Path: "/a.go", Line: 3, Old: "foo again", New: "bar again"},
		{Path: "/b.go", Line: 1, Old: "foo", New: "bar"}, // stale
		{Path: "/ro.go", Line: 1, Old: "foo", New: "bar"},
		{Path: "/gone.go", Line: 1, Old: "foo", New: "bar"},
	}
	res := Apply(changes, fs)
	if res.Files != 1 || res.Lines != 2 || len(res.Stale) != 1 || len(res.Errs) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if want := []string{"bar()", "keep", "bar again"}; !reflect.DeepEqual(fs.files["/a.go"], want) {
		t.Fatalf("a.go = %q", fs.files["/a.go"])
	}
	if fs.files["/b.go"][0] != "edited since" {
		t.Fatal("a stale line was clobbered")
	}
	if s := res.Summary(); !strings.Contains(s, "replaced 2 line(s) in 1 file(s)") || !strings.Contains(s, "1 skipped") {
		t.Fatalf("summary = %q", s)
	}
}
