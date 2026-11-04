package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

var blameNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// blameModel is openText with blame on and fake blame loaded for each
// line, so no git runs.
func blameModel(t *testing.T, text string, wrap bool) *Model {
	t.Helper()
	m := openText(t, text, wrap)
	m.prefs = func() Prefs { return Prefs{TabWidth: 4, LineNumbers: true, Wrap: wrap, Blame: true, Scrollback: 1000} }
	m.nowFn = func() time.Time { return blameNow }
	tab := m.bufs[m.activeTab]
	tab.blame = blameState{started: true, loaded: true}
	for _, ln := range tab.ed.Buffer().Lines() {
		tab.blame.lines = append(tab.blame.lines, gitcore.BlameLine{
			Hash: "abc", Author: "Ada", When: blameNow.Add(-50 * time.Hour), Subject: "tidy the parser", Text: ln,
		})
	}
	return m
}

func TestRelativeAge(t *testing.T) {
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "just now"}, {time.Minute, "1 minute ago"}, {5 * time.Hour, "5 hours ago"},
		{50 * time.Hour, "2 days ago"}, {20 * 24 * time.Hour, "2 weeks ago"},
		{90 * 24 * time.Hour, "3 months ago"}, {800 * 24 * time.Hour, "2 years ago"},
	} {
		if got := relativeAge(blameNow.Add(-c.ago), blameNow); got != c.want {
			t.Errorf("%v: %q, want %q", c.ago, got, c.want)
		}
	}
}

// F-143: the annotation follows the cursor and sits after the line.
func TestBlameShowsOnCursorLineOnly(t *testing.T) {
	m := blameModel(t, "first line\nsecond line\n", false)
	const want = "Ada, 2 days ago · tidy the parser"
	rows := strings.Split(plainView(m), "\n")
	var with []string
	for _, r := range rows {
		if strings.Contains(r, want) {
			with = append(with, r)
		}
	}
	if len(with) != 1 || !strings.Contains(with[0], "first line  "+want) {
		t.Fatalf("want one annotated row after the first line, got %q", with)
	}
	feed(m, "j")
	v := plainView(m)
	if strings.Count(v, want) != 1 || !strings.Contains(v, "second line  "+want) {
		t.Fatalf("annotation did not move with the cursor:\n%s", v)
	}
}

func TestBlameOffByDefaultAndSetCommands(t *testing.T) {
	m := openText(t, "first line\n", false)
	tab := m.bufs[m.activeTab]
	tab.blame = blameState{started: true, loaded: true, lines: []gitcore.BlameLine{{Hash: "abc", Author: "Ada", Subject: "x", Text: "first line"}}}
	if strings.Contains(plainView(m), "Ada") {
		t.Fatal("blame must be off by default")
	}
	if msg, ok := m.blameCommand("set blame"); !ok || msg != "blame: on" {
		t.Fatalf("set blame: %q %v", msg, ok)
	}
	if !strings.Contains(plainView(m), "Ada") {
		t.Fatal(":set blame did not turn it on")
	}
	m.blameCommand("set noblame")
	if strings.Contains(plainView(m), "Ada") {
		t.Fatal(":set noblame did not turn it off")
	}
	if _, ok := m.blameCommand("set wrap"); ok {
		t.Fatal("not a blame command")
	}
	// The session override beats the setting.
	m.prefs = func() Prefs { return Prefs{TabWidth: 4, LineNumbers: true, Blame: true, Scrollback: 1000} }
	if strings.Contains(plainView(m), "Ada") {
		t.Fatal("override must beat editor.blame")
	}
}

func TestBlameUncommittedAndEdits(t *testing.T) {
	m := blameModel(t, "one\ntwo\nthree\n", false)
	tab := m.bufs[m.activeTab]
	tab.blame.lines[1].Uncommitted = true
	feed(m, "j")
	if v := plainView(m); !strings.Contains(v, "two  not committed yet") {
		t.Fatalf("uncommitted line:\n%s", v)
	}
	// Insert a line above: the lines below keep their own blame.
	feed(m, "g", "g", "O")
	typeKeys(m, "zero")
	feed(m, "esc", "G")
	feed(m, "k") // "three"
	feed(m, "k") // "two"
	if v := plainView(m); !strings.Contains(v, "two  not committed yet") {
		t.Fatalf("shifted line kept its blame:\n%s", v)
	}
	feed(m, "k") // "one" is below the inserted line
	if v := plainView(m); !strings.Contains(v, "one  Ada") {
		t.Fatalf("line below the insertion:\n%s", v)
	}
	feed(m, "k") // the new line
	if v := plainView(m); !strings.Contains(v, "zero  not committed yet") {
		t.Fatalf("inserted line:\n%s", v)
	}
}

func TestBlameForMapping(t *testing.T) {
	s := &blameState{loaded: true}
	for _, x := range []string{"a", "b", "c", "d"} {
		s.lines = append(s.lines, gitcore.BlameLine{Author: x, Text: x})
	}
	cases := []struct {
		name string
		cur  []string
		l    int
		want string // author, "new" for uncommitted, "" for unknown
	}{
		{"unchanged", []string{"a", "b", "c", "d"}, 2, "c"},
		{"edited in place", []string{"a", "B", "c", "d"}, 1, "new"},
		{"line above an edit", []string{"a", "B", "c", "d"}, 0, "a"},
		{"insert shifts the tail", []string{"a", "x", "b", "c", "d"}, 4, "d"},
		{"inserted line", []string{"a", "x", "b", "c", "d"}, 1, "new"},
		{"delete shifts the tail", []string{"a", "c", "d"}, 2, "d"},
		{"replaced with a different count", []string{"a", "X", "Y", "d"}, 1, "new"},
		{"two edits, middle unknown", []string{"A", "b", "c", "D", "e"}, 2, ""},
	}
	for _, c := range cases {
		b, ok := blameFor(s, c.cur, c.l)
		got := ""
		switch {
		case !ok:
		case b.Uncommitted:
			got = "new"
		default:
			got = b.Author
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// F-143: with wrap on the note rides the last visual row, and is left
// out when that row has no room.
func TestBlameWrapLastRowOrOmitted(t *testing.T) {
	m := blameModel(t, words(30)+"\nsecond\n", true)
	const want = "Ada, 2 days ago"
	v := plainView(m)
	rows := strings.Split(v, "\n")
	idx := -1
	for i, r := range rows {
		if strings.Contains(r, want) {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("no annotation on a continuation row:\n%s", v)
	}
	if strings.Contains(rows[idx-1], want) || strings.Contains(rows[idx+1], want) {
		t.Fatal("annotation repeated on more than one row")
	}
	// A full last row leaves no room: the note is dropped, not wrapped.
	m = blameModel(t, "x\n", true)
	m = blameModel(t, strings.Repeat("x", 2*m.bufTextWidth(2))+"\n", true)
	if strings.Contains(plainView(m), "Ada") {
		t.Fatal("annotation must be omitted when the last row is full")
	}
}

func TestBlameSuffixFit(t *testing.T) {
	if blameSuffix("Ada, 2 days ago · tidy", 8) != "" {
		t.Fatal("too little room must omit")
	}
	got := plainOf(blameSuffix("Ada, 2 days ago · tidy the parser", 22))
	if got != "  Ada, 2 days ago…" && !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated suffix = %q", got)
	}
}

func plainOf(s string) string { return strings.TrimRight(ansi.Strip(s), "\n") }

// F-143 end to end with the user's git: the first View starts the blame
// off the UI goroutine, the result arrives as a message, and a save
// drops it and asks again.
func TestBlameWithRealGit(t *testing.T) {
	m, alpha := gitDepthEditor(t)
	file := filepath.Join(alpha, "app.go")
	m.open(&node{kind: nodeFile, path: file, name: "app.go"})
	m.prefs = func() Prefs { return Prefs{TabWidth: 4, LineNumbers: true, Blame: true, Scrollback: 1000} }
	m.nowFn = func() time.Time { return time.Unix(0, 0).Add(3 * time.Hour) } // go-git commits at the epoch here

	m.View() // starts the blame; the screen does not wait for it
	tab := m.bufs[m.activeTab]
	if tab.blame.loaded {
		t.Fatal("blame must not be loaded synchronously")
	}
	m.Update(waitMsg(t, m))
	if v := plainView(m); !strings.Contains(v, "line 1  t, 3 hours ago · initial") {
		t.Fatalf("committed line:\n%s", v)
	}

	// Save an edit: the blame is dropped and refetched, and the new line
	// reads as uncommitted.
	feed(m, "j", "o")
	typeKeys(m, "fresh")
	feed(m, "esc")
	typeKeys(m, ":w")
	feed(m, "enter")
	if tab.blame.loaded {
		t.Fatal("save must invalidate the cached blame")
	}
	m.View()
	m.Update(waitMsg(t, m))
	if v := plainView(m); !strings.Contains(v, "fresh  not committed yet") {
		t.Fatalf("saved but uncommitted line:\n%s", v)
	}
	if got, _ := os.ReadFile(file); !strings.Contains(string(got), "fresh") {
		t.Fatal("file not saved")
	}
}

// A result from before an invalidation must not land.
func TestBlameStaleResultDropped(t *testing.T) {
	m := blameModel(t, "one\n", false)
	tab := m.bufs[m.activeTab]
	tab.blame.invalidate()
	m.applyBlame(&blameResult{path: tab.path, gen: tab.blame.gen - 1, lines: []gitcore.BlameLine{{Text: "one"}}})
	if tab.blame.loaded {
		t.Fatal("stale result applied")
	}
}

func waitMsg(t *testing.T, m *Model) teaMsg {
	t.Helper()
	select {
	case msg := <-m.termMsgs:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("no blame message")
	}
	return teaMsg{}
}

func TestBlameGolden(t *testing.T) {
	m := blameModel(t, "package main\n\n// "+words(45)+"\n\nfunc main() {\n}\n", false)
	feed(m, "j", "j", "j", "j") // a short line, so the note fits
	golden.Snapshot(t, "editor_blame_100x30", m.View())
	w := blameModel(t, "package main\n\n// "+words(45)+"\n\nfunc main() {\n}\n", true)
	feed(w, "j", "j")
	golden.Snapshot(t, "editor_blame_wrap_100x30", w.View())
}
