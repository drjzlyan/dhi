package vt

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
)

func plain(lines []string) string {
	return ansi.Strip(strings.Join(lines, "\n"))
}

func TestShellOutputAndColors(t *testing.T) {
	s := New(100)
	s.Resize(30, 4)
	s.Feed([]byte("$ ls\r\n\x1b[32mgreen\x1b[0m file\r\n$ "))
	lines := s.Lines(30, 4)
	if len(lines) != 4 {
		t.Fatalf("rows = %d", len(lines))
	}
	if got := plain(lines); !strings.Contains(got, "$ ls") || !strings.Contains(got, "green file") {
		t.Fatalf("screen:\n%s", got)
	}
	if !strings.Contains(lines[1], "\x1b[32m") {
		t.Fatalf("colors must pass through: %q", lines[1])
	}
	if x, y := s.Cursor(); x != 2 || y != 2 {
		t.Fatalf("cursor = %d,%d want 2,2", x, y)
	}
}

// TestAltScreenProgram pins F-061: a full-screen program draws on the
// alternate screen and leaving it restores the shell's screen.
func TestAltScreenProgram(t *testing.T) {
	s := New(100)
	s.Resize(20, 5)
	s.Feed([]byte("$ vim\r\n"))
	s.Feed([]byte("\x1b[?1049h\x1b[2J\x1b[H~ one\x1b[5;1H-- INSERT --"))
	if !s.AltScreen() {
		t.Fatal("alt screen not entered")
	}
	got := plain(s.Lines(20, 5))
	if !strings.Contains(got, "~ one") || !strings.Contains(got, "-- INSERT --") || strings.Contains(got, "$ vim") {
		t.Fatalf("alt screen:\n%s", got)
	}
	s.Feed([]byte("\x1b[?1049l"))
	if s.AltScreen() || !strings.Contains(plain(s.Lines(20, 5)), "$ vim") {
		t.Fatalf("leaving the alt screen must restore the shell:\n%s", plain(s.Lines(20, 5)))
	}
}

func TestLinesFitTheRequestedBox(t *testing.T) {
	s := New(100)
	s.Resize(10, 3)
	s.Feed([]byte(strings.Repeat("x", 25) + "\r\nshort\tTAB"))
	for _, l := range s.Lines(8, 3) {
		if ansi.Width(l) > 8 || strings.Contains(l, "\t") {
			t.Fatalf("row escapes its box: %q", l)
		}
	}
	if n := len(s.Lines(8, 6)); n != 6 {
		t.Fatalf("rows = %d, want 6", n)
	}
}

func TestRepliesToTerminalQueries(t *testing.T) {
	s := New(100)
	s.Resize(20, 5)
	defer s.Close()
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := s.Replies().Read(buf)
		got <- string(buf[:n])
	}()
	s.Feed([]byte("\x1b[6n")) // DSR: where is the cursor?
	if reply := <-got; !strings.HasPrefix(reply, "\x1b[1;1R") {
		t.Fatalf("cursor report = %q", reply)
	}
}
