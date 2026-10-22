// Package vt is the terminal drawer's screen (F-061): a full VT emulator
// (charmbracelet/x/vt, ADR-0030) sized exactly to the visible pane, so
// full-screen programs — vim, htop, less, git's pager — run in the
// drawer with their alternate screen, scroll regions and cursor moves.
// Session colors are CONTENT and pass through (the theme-only color rule
// governs DHI's chrome, not what a program prints).
package vt

import (
	"encoding/base64"
	"io"
	"strings"

	xvt "github.com/charmbracelet/x/vt"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// Screen is one session's emulated terminal.
type Screen struct {
	emu  *xvt.SafeEmulator
	w, h int
}

// New returns an 80x24 screen keeping up to capacity lines of history.
func New(capacity int) *Screen {
	if capacity < 10 {
		capacity = 10
	}
	s := &Screen{emu: xvt.NewSafeEmulator(80, 24), w: 80, h: 24}
	s.emu.SetScrollbackSize(capacity)
	return s
}

// Feed interprets program output.
func (s *Screen) Feed(chunk []byte) { _, _ = s.emu.Write(chunk) }

// Replies is what the emulated terminal answers to the program (cursor
// position reports, device attributes, colour queries): the caller
// copies it to the session's input, or programs like vim wait forever.
// Reads block until there is a reply or the screen is closed.
func (s *Screen) Replies() io.Reader { return s.emu }

// Close ends Replies readers. It closes the reply pipe's writer rather
// than calling Emulator.Close, which flips an unsynchronized flag that a
// blocked Read checks (a data race under -race).
func (s *Screen) Close() {
	if c, ok := s.emu.InputPipe().(io.Closer); ok {
		_ = c.Close()
	}
}

// Resize sets the emulated size (keep it equal to the session's PTY).
func (s *Screen) Resize(w, h int) {
	w, h = max(w, 2), max(h, 1)
	if w == s.w && h == s.h {
		return
	}
	s.w, s.h = w, h
	s.emu.Resize(w, h)
}

// Size reports the emulated size.
func (s *Screen) Size() (w, h int) { return s.w, s.h }

// AltScreen reports a full-screen program owning the terminal.
func (s *Screen) AltScreen() bool { return s.emu.IsAltScreen() }

// Cursor is the cursor cell (column, row) on the visible screen.
func (s *Screen) Cursor() (x, y int) {
	p := s.emu.CursorPosition()
	return p.X, p.Y
}

// Lines renders the visible screen as exactly height rows of at most
// width cells, styles intact and closed at the end of each row.
func (s *Screen) Lines(width, height int) []string {
	if height <= 0 {
		return nil
	}
	rows := strings.Split(s.emu.Render(), "\n")
	out := make([]string, 0, height)
	for _, r := range rows {
		if len(out) == height {
			break
		}
		r = strings.ReplaceAll(r, "\t", " ")
		if ansi.Width(r) > width {
			r = ansi.Clip(r, width)
		}
		out = append(out, r+"\x1b[0m")
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// Paste sends text to the program as a paste: wrapped in bracketed-paste
// markers when the program turned that mode on (shells, vim), so it is
// not run line by line. The text goes out through Replies.
func (s *Screen) Paste(text string) { s.emu.Paste(text) }

// OnClipboard calls f with the text a program copies through OSC 52
// (tmux, vim's "+y over SSH, neovim): the drawer forwards it to the
// system clipboard. Read requests ("?") are ignored; DHI never hands the
// clipboard to a program.
func (s *Screen) OnClipboard(f func(text string)) {
	s.emu.RegisterOscHandler(52, func(data []byte) bool {
		// data is "52;<selection>;<base64>"; older forms omit the code.
		parts := strings.SplitN(string(data), ";", 3)
		payload := parts[len(parts)-1]
		if payload == "?" || payload == "" {
			return true
		}
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return true
		}
		f(string(b))
		return true
	})
}
