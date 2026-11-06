package vt

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseMark(t *testing.T) {
	cases := []struct {
		in   string
		want Mark
		ok   bool
	}{
		{"133;A", Mark{Kind: 'A'}, true},
		{"133;B", Mark{Kind: 'B'}, true},
		{"133;C", Mark{Kind: 'C'}, true},
		{"133;D;0", Mark{Kind: 'D', HasExit: true}, true},
		{"133;D;127", Mark{Kind: 'D', Exit: 127, HasExit: true}, true},
		{"133;D", Mark{Kind: 'D'}, true},
		{"133;D;1;aid=7", Mark{Kind: 'D', Exit: 1, HasExit: true}, true},
		{"133;A;aid=7", Mark{Kind: 'A'}, true},
		{"D;2", Mark{Kind: 'D', Exit: 2, HasExit: true}, true},
		{"133;Z", Mark{}, false},
		{"133;", Mark{}, false},
		{"133", Mark{}, false},
	}
	for _, c := range cases {
		got, ok := ParseMark([]byte(c.in))
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseMark(%q) = %+v,%v want %+v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseCwd(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"7;file:///Users/a/proj", "/Users/a/proj", true},
		{"7;file://localhost/tmp/x%20y", "/tmp/x y", true},
		{"7;file://some-other-box.example/tmp", "", false},
		{"7;http://localhost/tmp", "", false},
		{"7;/just/a/path", "", false},
		{"7;file://", "", false},
	}
	for _, c := range cases {
		got, ok := ParseCwd([]byte(c.in))
		if ok != c.ok || got != c.want {
			t.Errorf("ParseCwd(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// feedSplit feeds s in pieces of n bytes, so sequences straddle reads.
func feedSplit(s *Screen, in string, n int) {
	for i := 0; i < len(in); i += n {
		s.Feed([]byte(in[i:min(i+n, len(in))]))
	}
}

const session = "\x1b]133;A\x07$ true\r\n\x1b]133;D;0\x07\x1b]133;A\x1b\\$ false\r\n\x1b]133;D;1\x07\x1b]133;A\x07$ "

func TestMarksTerminatorsAndSplitReads(t *testing.T) {
	for _, n := range []int{1, 2, 3, 7, 1000} {
		s := New(100)
		s.Resize(30, 10)
		feedSplit(s, session, n)
		cmds := s.Commands()
		if len(cmds) != 3 {
			t.Fatalf("chunk %d: commands = %+v", n, cmds)
		}
		if cmds[0].Failed() || !cmds[0].Done {
			t.Errorf("chunk %d: first should succeed: %+v", n, cmds[0])
		}
		if !cmds[1].Failed() || cmds[1].Exit != 1 {
			t.Errorf("chunk %d: second should fail with 1: %+v", n, cmds[1])
		}
		if cmds[2].Done {
			t.Errorf("chunk %d: third still running: %+v", n, cmds[2])
		}
		if cmds[0].Row != 0 || cmds[1].Row != 1 || cmds[2].Row != 2 {
			t.Errorf("chunk %d: rows = %d %d %d", n, cmds[0].Row, cmds[1].Row, cmds[2].Row)
		}
		if !s.Integrated() {
			t.Errorf("chunk %d: not integrated", n)
		}
		if got := plain(s.Lines(30, 10)); !strings.Contains(got, "$ false") || strings.Contains(got, "133") {
			t.Errorf("chunk %d: marks must not print:\n%s", n, got)
		}
	}
}

func TestMarksNotSeenWithoutShellIntegration(t *testing.T) {
	s := New(100)
	s.Feed([]byte("$ ls\r\nfile\r\n$ "))
	if s.Integrated() || len(s.Commands()) != 0 {
		t.Fatalf("plain output must leave no marks: %+v", s.Commands())
	}
}

func TestCwdFromOSC7(t *testing.T) {
	for _, term := range []string{"\x07", "\x1b\\"} {
		s := New(100)
		feedSplit(s, "\x1b]7;file:///tmp/work%20dir"+term, 4)
		if s.Cwd() != "/tmp/work dir" {
			t.Fatalf("term %q: cwd = %q", term, s.Cwd())
		}
	}
	s := New(100)
	s.Feed([]byte("\x1b]7;file://elsewhere.example/srv\x07"))
	if s.Cwd() != "" {
		t.Fatalf("a remote host's path must be ignored, got %q", s.Cwd())
	}
}

func TestMarkRowsSurviveScrollbackTrim(t *testing.T) {
	s := New(10)
	s.Resize(20, 4)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "\x1b]133;A\x07p%d\r\n\x1b]133;D;%d\x07", i, i%2)
	}
	s.Feed([]byte(b.String()))
	cmds := s.Commands()
	if len(cmds) == 0 || len(cmds) > 10+4 {
		t.Fatalf("commands = %d, want only those still in scrollback", len(cmds))
	}
	// every mark's row must still be the line it was set on
	hist := s.History(20)
	base := s.RowBase() - len(hist)
	for _, c := range cmds {
		i := c.Row - base
		var line string
		if i < len(hist) {
			line = plain([]string{hist[i]})
		} else {
			line = plain(s.Lines(20, 4)[i-len(hist) : i-len(hist)+1])
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "p") {
			t.Fatalf("row %d (idx %d) = %q, want a prompt line", c.Row, i, line)
		}
		// the prompt text names the command; its exit parity matches
		var n int
		fmt.Sscanf(strings.TrimSpace(line), "p%d", &n)
		if c.Failed() != (n%2 == 1) {
			t.Fatalf("row %d: %q failed=%v", c.Row, line, c.Failed())
		}
	}
}

func TestMarksIgnoredOnAltScreen(t *testing.T) {
	s := New(100)
	s.Feed([]byte("\x1b[?1049h\x1b]133;A\x07"))
	if len(s.Commands()) != 0 {
		t.Fatal("alt-screen marks must be ignored")
	}
}
