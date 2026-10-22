package vt

import (
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"
)

// readReply collects what the screen sends back to the program.
func readReply(t *testing.T, s *Screen, want int) string {
	t.Helper()
	buf := make([]byte, 256)
	got := ""
	deadline := time.After(2 * time.Second)
	done := make(chan struct{})
	go func() {
		for len(got) < want {
			n, err := s.Replies().Read(buf)
			got += string(buf[:n])
			if err == io.EOF || err != nil {
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-deadline:
		t.Fatalf("timed out; got %q", got)
	}
	return got
}

func TestPasteIsBracketedWhenTheProgramAsks(t *testing.T) {
	s := New(10)
	defer s.Close()
	s.Feed([]byte("\x1b[?2004h")) // the shell turns bracketed paste on
	go s.Paste("ls\nrm")
	got := readReply(t, s, len("\x1b[200~ls\nrm\x1b[201~"))
	if got != "\x1b[200~ls\nrm\x1b[201~" {
		t.Fatalf("paste = %q", got)
	}
}

func TestOSC52CopyReachesTheCallback(t *testing.T) {
	s := New(10)
	defer s.Close()
	var copied string
	s.OnClipboard(func(text string) { copied = text })
	b64 := base64.StdEncoding.EncodeToString([]byte("from tmux"))
	s.Feed([]byte("\x1b]52;c;" + b64 + "\x07"))
	if copied != "from tmux" {
		t.Fatalf("copied %q", copied)
	}
	copied = ""
	s.Feed([]byte("\x1b]52;c;?\x07")) // a read request is never answered
	if copied != "" || strings.Contains(copied, "?") {
		t.Fatal("a clipboard read request must be ignored")
	}
}
