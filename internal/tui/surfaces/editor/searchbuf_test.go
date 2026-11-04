package editor

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// globSearcher is a scripted searcher that also takes a path glob.
type globSearcher struct {
	scriptedSearcher
	glob    string
	pattern string
}

func (g *globSearcher) SearchGlob(ctx context.Context, pattern string, roots []string, glob string) (<-chan search.Hit, error) {
	g.glob, g.pattern = glob, pattern
	return g.Search(ctx, pattern, roots)
}

const sbMain = "line one\nline two\nneedle three\nline four\nline five\nline six\nline seven\nneedle eight\nline nine\n"

// sbSetup is a workspace whose alpha/src/main.go holds sbMain, with a
// searcher that finds the two needle lines.
func sbSetup(t *testing.T) (*Model, *globSearcher, string) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	abs := vpathAbs(t, ws, "alpha/src/main.go")
	if err := os.WriteFile(abs, []byte(sbMain), 0o644); err != nil {
		t.Fatal(err)
	}
	gs := &globSearcher{scriptedSearcher: scriptedSearcher{hits: []search.Hit{
		{Path: abs, Line: 3, Text: "needle three"},
		{Path: abs, Line: 8, Text: "needle eight"},
	}}}
	m := New("test", ws, WithSearcher(gs))
	m.Resize(100, 30)
	return m, gs, abs
}

// ex runs an ex command through the keyboard.
func ex(m *Model, cmd string) {
	m.HandleKey(":")
	for _, r := range cmd {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
}

// finishQueued delivers the queued background result to the model.
func finishQueued(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.TakeCmd()
	if cmd == nil {
		t.Fatal("no background work was queued")
	}
	m.Update(cmd())
}

// grep opens the search buffer for pattern and returns its editor.
func grep(t *testing.T, m *Model, args string) *textbuf.Editor {
	t.Helper()
	// a buffer must be focused for ":" to reach the ex line
	m.open(&node{kind: nodeFile, path: vpathAbs(t, m.ws, "alpha/app.go"), name: "app.go"})
	ex(m, "grep "+args)
	finishQueued(t, m)
	e := m.active()
	if e == nil || !m.isSearchBuf(e) {
		t.Fatalf("search buffer not open; message %q", func() string {
			if e != nil {
				return e.Message()
			}
			return ""
		}())
	}
	return e
}

func TestParseGrep(t *testing.T) {
	cases := []struct {
		in      string
		pattern string
		glob    string
		ctx     int
		bad     bool
	}{
		{"foo", "foo", "", 2, false},
		{"foo *.go", "foo", "*.go", 2, false},
		{`"two words" src/**`, "two words", "src/**", 2, false},
		{"-C 5 foo", "foo", "", 5, false},
		{"-C0 foo", "foo", "", 0, false},
		{"-C x foo", "", "", 0, true},
		{`"open foo`, "", "", 0, true},
		{"a b c", "", "", 0, true},
		{"", "", "", 2, false},
	}
	for _, c := range cases {
		g, err := parseGrep(c.in)
		if (err != nil) != c.bad {
			t.Errorf("%q: err = %v", c.in, err)
			continue
		}
		if c.bad {
			continue
		}
		if g.pattern != c.pattern || g.glob != c.glob || g.ctx != c.ctx || !g.regex {
			t.Errorf("%q: got %+v", c.in, g)
		}
	}
}

func TestBuildSearchBufContextAndGroups(t *testing.T) {
	src := strings.Split(strings.TrimSuffix(sbMain, "\n"), "\n")
	hits := []search.Hit{{Path: "/w/b.go", Line: 8, Text: "needle eight"}, {Path: "/w/a.go", Line: 3, Text: "needle three"}, {Path: "/w/a.go", Line: 8, Text: "needle eight"}}
	text, sb, matches, files := buildSearchBuf(hits, 1,
		func(p string) string { return strings.TrimPrefix(p, "/w/") },
		func(string) ([]string, error) { return src, nil })
	want := strings.Join([]string{
		"a.go:",
		"  2- line two", "  3: needle three", "  4- line four",
		searchSep,
		"  7- line seven", "  8: needle eight", "  9- line nine",
		"b.go:",
		"  7- line seven", "  8: needle eight", "  9- line nine",
	}, "\n")
	if text != want {
		t.Errorf("text:\n%s\nwant:\n%s", text, want)
	}
	if matches != 3 || files != 2 {
		t.Errorf("matches %d files %d", matches, files)
	}
	if sb.rows["/w/a.go"][2] != "line two" || sb.paths["b.go"] != "/w/b.go" {
		t.Errorf("snapshot = %+v", sb)
	}
	// Adjacent windows merge into one group, no separator.
	text, _, _, _ = buildSearchBuf(hits[1:], 2,
		func(p string) string { return p },
		func(string) ([]string, error) { return src, nil })
	if strings.Contains(text, searchSep) {
		t.Errorf("windows 1-5 and 6-9 touch, expected one group:\n%s", text)
	}
}

func TestPlanSearchEdits(t *testing.T) {
	sb := &searchBuf{
		rows:  map[string]map[int]string{"/w/a.go": {2: "line two", 3: "needle three"}},
		paths: map[string]string{"a.go": "/w/a.go"},
	}
	plan, ignored := sb.planSearchEdits([]string{
		"# note", "a.go:",
		"  2- line two",         // untouched
		"  3: needle CHANGED",   // edited
		"  9: invented row",     // not listed
		"stray line",            // belongs to no row
		"nofile.go:",            // unknown header
		"  3: after bad header", // no file in scope
		"",
	})
	if len(plan) != 1 || plan[0].Path != "/w/a.go" || plan[0].Line != 3 || plan[0].Old != "needle three" || plan[0].New != "needle CHANGED" {
		t.Errorf("plan = %+v", plan)
	}
	if ignored != 4 {
		t.Errorf("ignored = %d, want 4", ignored)
	}
	// Context rows edit too; one trailing space after the colon is the separator only.
	plan, _ = sb.planSearchEdits([]string{"a.go:", "  2-   indented"})
	if len(plan) != 1 || plan[0].New != "  indented" {
		t.Errorf("context edit plan = %+v", plan)
	}
}

func TestGrepOpensBufferWithGlobAndContext(t *testing.T) {
	m, gs, _ := sbSetup(t)
	e := grep(t, m, "-C 1 needle *.go")
	if gs.glob != "*.go" || gs.pattern != "needle" {
		t.Errorf("searcher got %q %q", gs.pattern, gs.glob)
	}
	text := e.Buffer().Text()
	for _, want := range []string{"alpha/src/main.go:", "  2- line two", "  3: needle three", "  8: needle eight", "2 matches in 1 file"} {
		if !strings.Contains(text, want) {
			t.Errorf("buffer missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(plainView(m), "needle three") {
		t.Errorf("view:\n%s", plainView(m))
	}
}

func TestGrepNoMatchesAndMissingRipgrep(t *testing.T) {
	m, gs, _ := sbSetup(t)
	gs.hits = nil
	m.open(&node{kind: nodeFile, path: vpathAbs(t, m.ws, "alpha/app.go"), name: "app.go"})
	ex(m, "grep zzz")
	finishQueued(t, m)
	if got := m.active().Message(); !strings.Contains(got, "no matches") {
		t.Errorf("message = %q", got)
	}
	if m.sbuf != nil {
		t.Error("an empty search opened a buffer")
	}

	gs.err = &search.MissingToolError{Tool: "rg"}
	ex(m, "grep zzz")
	if got := m.active().Message(); !strings.Contains(got, "rg is not installed") {
		t.Errorf("missing ripgrep message = %q", got)
	}
}

func TestGrepNeedsGlobSearcher(t *testing.T) {
	m := newSearchEditor(t, &scriptedSearcher{})
	m.open(&node{kind: nodeFile, path: vpathAbs(t, m.ws, "alpha/app.go"), name: "app.go"})
	ex(m, "grep x")
	if got := m.active().Message(); !strings.Contains(got, "not available") {
		t.Errorf("message = %q", got)
	}
}

func TestGrepCapsMatches(t *testing.T) {
	m, gs, abs := sbSetup(t)
	gs.hits = nil
	if err := os.WriteFile(abs, []byte(strings.Repeat("x\n", searchBufMaxMatches+5)), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= searchBufMaxMatches+5; i++ {
		gs.hits = append(gs.hits, search.Hit{Path: abs, Line: i, Text: "x"})
	}
	e := grep(t, m, "x")
	if !strings.Contains(e.Buffer().Text(), "showing the first 1000 matches") {
		t.Errorf("no cap note:\n%.300s", e.Buffer().Text())
	}
	if n := strings.Count(e.Buffer().Text(), ": x\n"); n != searchBufMaxMatches {
		t.Errorf("%d match rows, want %d", n, searchBufMaxMatches)
	}
}

func TestSearchBufEnterAndGdOpenRow(t *testing.T) {
	for _, jump := range [][]string{{"enter"}, {"g", "d"}} {
		m, _, abs := sbSetup(t)
		e := grep(t, m, "needle")
		for i := 0; i < e.Buffer().LineCount(); i++ {
			if e.Buffer().Line(i) == "  8: needle eight" {
				e.Buffer().SetCursor(textbuf.Pos{Line: i})
			}
		}
		feed(m, jump...)
		if a := m.active(); a == nil || a.Path() != abs {
			t.Fatalf("%v: active = %v", jump, a)
		}
		if c := m.active().Buffer().Cursor(); c.Line != 7 || c.Col != 0 {
			t.Errorf("%v: cursor = %+v, want line 8", jump, c)
		}
	}
}

func TestSearchBufWriteAsksThenApplies(t *testing.T) {
	m, _, abs := sbSetup(t)
	e := grep(t, m, "needle")
	idx := func(line string) int {
		for i := 0; i < e.Buffer().LineCount(); i++ {
			if e.Buffer().Line(i) == line {
				return i
			}
		}
		t.Fatalf("no line %q in\n%s", line, e.Buffer().Text())
		return -1
	}
	e.Buffer().ReplaceLines(map[int]string{
		idx("  3: needle three"): "  3: thread three",
		idx("  8: needle eight"): "  8: thread eight",
	})
	ex(m, "w")
	if m.sbApply == nil || len(m.sbApply.plan) != 2 {
		t.Fatalf("no confirm card after :w; message %q", e.Message())
	}
	if disk, _ := os.ReadFile(abs); string(disk) != sbMain {
		t.Fatal("the source changed before the card was answered")
	}
	if v := plainView(m); !strings.Contains(v, "2 edited lines to 1 file") || !strings.Contains(v, "alpha/src/main.go") {
		t.Errorf("card:\n%s", v)
	}

	// n cancels: nothing written, rows stay edited.
	m.HandleKey("n")
	if m.sbApply != nil {
		t.Fatal("card still open")
	}
	if disk, _ := os.ReadFile(abs); string(disk) != sbMain {
		t.Fatal("cancel wrote the file")
	}

	// A second :w asks again; y applies.
	ex(m, "w")
	m.HandleKey("y")
	disk, _ := os.ReadFile(abs)
	want := strings.Replace(strings.Replace(sbMain, "needle three", "thread three", 1), "needle eight", "thread eight", 1)
	if string(disk) != want {
		t.Errorf("source after apply:\n%s", disk)
	}
	if !strings.Contains(e.Message(), "applied 2 lines in 1 file") {
		t.Errorf("message = %q", e.Message())
	}
	// Applied rows are the new baseline: writing again has nothing to apply.
	ex(m, "w")
	if m.sbApply != nil {
		t.Error("applied rows were offered again")
	}
}

func TestSearchBufSkipsChangedSource(t *testing.T) {
	m, _, abs := sbSetup(t)
	e := grep(t, m, "needle")
	for i := 0; i < e.Buffer().LineCount(); i++ {
		switch e.Buffer().Line(i) {
		case "  3: needle three":
			e.Buffer().ReplaceLines(map[int]string{i: "  3: thread three"})
		case "  8: needle eight":
			e.Buffer().ReplaceLines(map[int]string{i: "  8: thread eight"})
		}
	}
	// Someone else edits line 3 after the search.
	changed := strings.Replace(sbMain, "needle three", "needle 3 by someone else", 1)
	if err := os.WriteFile(abs, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	ex(m, "w")
	m.HandleKey("y")
	disk, _ := os.ReadFile(abs)
	want := strings.Replace(changed, "needle eight", "thread eight", 1)
	if string(disk) != want {
		t.Errorf("source:\n%s", disk)
	}
	msg := e.Message()
	if !strings.Contains(msg, "applied 1 line") || !strings.Contains(msg, "1 skipped") || !strings.Contains(msg, "alpha/src/main.go:3") {
		t.Errorf("message = %q", msg)
	}
}

func TestSearchBufApplyEditsOpenBufferInPlace(t *testing.T) {
	m, _, abs := sbSetup(t)
	e := grep(t, m, "needle")
	m.open(&node{kind: nodeFile, path: abs, name: "main.go"})
	src := m.active()
	m.open(&node{kind: nodeFile, path: m.searchBufPath(), name: searchBufName})
	for i := 0; i < e.Buffer().LineCount(); i++ {
		if e.Buffer().Line(i) == "  3: needle three" {
			e.Buffer().ReplaceLines(map[int]string{i: "  3: thread three"})
		}
	}
	ex(m, "w")
	m.HandleKey("y")
	if src.Buffer().Line(2) != "thread three" || !src.Buffer().Dirty() {
		t.Errorf("open buffer line = %q dirty %v", src.Buffer().Line(2), src.Buffer().Dirty())
	}
	if disk, _ := os.ReadFile(abs); string(disk) != sbMain {
		t.Error("an open buffer's file was written behind its back")
	}
}

func TestGrepBangRefreshesAndPlainGrepKeepsEdits(t *testing.T) {
	m, gs, _ := sbSetup(t)
	e := grep(t, m, "needle")
	e.Buffer().ReplaceLines(map[int]string{3: "  3: edited"})
	ex(m, "grep other")
	if got := e.Message(); !strings.Contains(got, "not written") {
		t.Errorf("dirty buffer was replaced: %q", got)
	}
	if m.grepLast.pattern != "needle" {
		t.Errorf("grepLast = %+v", m.grepLast)
	}
	gs.hits = gs.hits[:1]
	ex(m, "grep!")
	finishQueued(t, m)
	if gs.pattern != "needle" {
		t.Errorf("refresh pattern = %q", gs.pattern)
	}
	e = m.active()
	if strings.Contains(e.Buffer().Text(), "edited") || strings.Contains(e.Buffer().Text(), "needle eight") || e.Buffer().Dirty() {
		t.Errorf("refresh kept old content:\n%s", e.Buffer().Text())
	}
	if n := len(m.bufs); n != 2 {
		t.Errorf("%d tabs after refresh, want the same search tab reused", n)
	}
}

func TestResultsPaneOpensBuffer(t *testing.T) {
	m, gs, _ := sbSetup(t)
	m.HandleKey("s")
	for _, r := range "needle" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	for _, h := range gs.hits {
		m.Update(hitMsg(h))
	}
	m.Update(searchDoneMsg{})
	if !strings.Contains(plainView(m), "o as buffer") {
		t.Errorf("results footer lacks the key:\n%s", plainView(m))
	}
	m.HandleKey("o")
	e := m.active()
	if e == nil || !m.isSearchBuf(e) {
		t.Fatal("o did not open the search buffer")
	}
	if !strings.Contains(e.Buffer().Text(), "  3: needle three") || m.mode != modeNav {
		t.Errorf("buffer:\n%s", e.Buffer().Text())
	}
}

func TestSearchBufNeverSavesSourceDirectly(t *testing.T) {
	m, _, _ := sbSetup(t)
	e := grep(t, m, "needle")
	if got, _ := workspace.ParseVPath("alpha/src/main.go"); got.String() == "" {
		t.Fatal("vpath")
	}
	// autosave skips the search buffer: a half-edited listing must not ask.
	e.Buffer().ReplaceLines(map[int]string{3: "  3: half edited"})
	m.autosaveTab(m.bufferFor(m.searchBufPath()))
	if m.sbApply != nil {
		t.Error("autosave opened the apply card")
	}
}

func TestSearchBufGoldens(t *testing.T) {
	m, _, _ := sbSetup(t)
	m.Resize(100, 30)
	e := grep(t, m, "needle")
	e.Buffer().SetCursor(textbuf.Pos{Line: 0})
	golden.Snapshot(t, "editor_search_buffer_100x30", m.View())
	for i := 0; i < e.Buffer().LineCount(); i++ {
		if e.Buffer().Line(i) == "  3: needle three" {
			e.Buffer().ReplaceLines(map[int]string{i: "  3: thread three"})
		}
	}
	ex(m, "w")
	golden.Snapshot(t, "editor_search_apply_card_100x30", m.View())
}
