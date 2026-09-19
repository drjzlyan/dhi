package vt

import (
	"strings"
	"testing"
)

func TestFeedKeepsColorsAndWrapsHistory(t *testing.T) {
	s := New(100)
	s.Feed([]byte("\x1b[31mred\x1b[0m plain\nnext line\n"))
	lines := s.Lines(80, 10)
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if !strings.Contains(lines[0], "\x1b[31m") {
		t.Fatalf("SGR lost: %q", lines[0])
	}
	if got := Width(lines[0]); got != 9 {
		t.Fatalf("width = %d, want 9", got)
	}
	if strings.Contains(lines[1], "\x1b[") {
		t.Fatalf("plain line styled: %q", lines[1])
	}
}

func TestCursorOverwrite(t *testing.T) {
	s := New(100)
	s.Feed([]byte("loading 10%\rloading 99%\n"))
	lines := s.Lines(80, 10)
	if got := lines[0]; got != "loading 99%" {
		t.Fatalf("CR overwrite = %q", got)
	}
}

func TestCUPPositionsAndGrows(t *testing.T) {
	s := New(100)
	s.Feed([]byte("one\ntwo\nthree\n"))
	// Jump to row 2 col 1 and rewrite.
	s.Feed([]byte("\x1b[3;1H"))
	s.Feed([]byte("THREE"))
	lines := s.Lines(80, 10)
	if got := lines[2]; got != "THREE" {
		t.Fatalf("CUP rewrite = %q", got)
	}
	if got := lines[0]; got != "one" {
		t.Fatalf("row 1 = %q", got)
	}
}

func TestEraseLineAndDisplay(t *testing.T) {
	s := New(100)
	s.Feed([]byte("hello world\nnext\n"))
	s.Feed([]byte("\x1b[1;1H\x1b[K")) // clear line 1 to EOL
	if got := s.Lines(80, 10)[0]; got != "" {
		t.Fatalf("EL after cursor-line start = %q", got)
	}
	s.Feed([]byte("\x1b[2J"))
	if got := strings.Join(s.Lines(80, 10), "|"); got != "" {
		t.Fatalf("ED2 = %q", got)
	}
}

func TestScrollbackCapTrimsHistory(t *testing.T) {
	s := New(12)
	for i := 0; i < 40; i++ {
		s.Feed([]byte("line\n"))
	}
	lines := s.Lines(80, 100)
	if len(lines) > 12 {
		t.Fatalf("cap exceeded: %d", len(lines))
	}
	if got := lines[len(lines)-1]; got != "line" {
		t.Fatalf("tail = %q", got)
	}
}

func TestOSCAndUnknownEscapesIgnored(t *testing.T) {
	s := New(10)
	s.Feed([]byte("\x1b]0;title\x07ok\x1b(B\x1b[?25h!\n"))
	if got := s.Lines(80, 10)[0]; got != "ok!" {
		t.Fatalf("osc/unknown handling = %q", got)
	}
}

func TestTabAligns(t *testing.T) {
	s := New(10)
	s.Feed([]byte("a\tb\n"))
	if got := s.Lines(80, 10)[0]; got != "a       b" {
		t.Fatalf("tab = %q", got)
	}
}
