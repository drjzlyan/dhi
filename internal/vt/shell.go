package vt

import (
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Shell integration (F-157): shells and prompt tools that already emit
// the FinalTerm / iTerm2 semantic-prompt marks (OSC 133 A/B/C/D) and the
// working-directory report (OSC 7) let the drawer jump between commands,
// flag failed ones and open new tabs where the shell is. DHI never
// writes a shell rc file (ADR-0044): without the marks these features
// stay idle and the drawer says how to turn them on.

// Mark is one parsed OSC 133 sequence.
type Mark struct {
	Kind    byte // 'A' prompt start, 'B' prompt end, 'C' output start, 'D' command end
	Exit    int  // D only: the command's exit status
	HasExit bool // D carried a numeric status
}

// ParseMark reads an OSC 133 payload ("133;D;1", with or without the
// leading code). Unknown kinds and other codes are refused.
func ParseMark(data []byte) (Mark, bool) {
	parts := strings.Split(string(data), ";")
	if len(parts) > 0 && parts[0] == "133" {
		parts = parts[1:]
	}
	if len(parts) == 0 || len(parts[0]) != 1 || !strings.Contains("ABCD", parts[0]) {
		return Mark{}, false
	}
	m := Mark{Kind: parts[0][0]}
	if m.Kind == 'D' && len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			m.Exit, m.HasExit = n, true
		}
	}
	return m, true
}

// ParseCwd reads an OSC 7 payload ("7;file://host/path") into a local
// path. A report from another machine (an ssh session) is refused: its
// path means nothing here.
func ParseCwd(data []byte) (string, bool) {
	s := strings.TrimPrefix(string(data), "7;")
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "file" || !strings.HasPrefix(u.Path, "/") {
		return "", false
	}
	if !localHost(u.Hostname()) {
		return "", false
	}
	return u.Path, true
}

func localHost(h string) bool {
	if h == "" || strings.EqualFold(h, "localhost") {
		return true
	}
	me, err := os.Hostname()
	if err != nil {
		return false
	}
	short := func(s string) string { return strings.ToLower(strings.SplitN(s, ".", 2)[0]) }
	return short(h) == short(me)
}

// Command is one prompt the shell marked. Row is absolute: it counts
// from the first line the session ever scrolled away, so it stays put
// while scrollback is trimmed (compare with RowBase).
type Command struct {
	Row     int
	Done    bool // the shell reported the command's end
	Exit    int
	HasExit bool
}

// Failed reports a finished command with a non-zero status.
func (c Command) Failed() bool { return c.Done && c.HasExit && c.Exit != 0 }

// scrollHeadroom lets a chunk push many lines before the trim in
// settle, which is how the screen counts what scrolled away.
const scrollHeadroom = 1 << 16

// onMark records a semantic-prompt mark. It runs inside Feed with the
// emulator locked, so it reads the unsynchronised emulator.
func (s *Screen) onMark(data []byte) bool {
	mk, ok := ParseMark(data)
	if !ok {
		return false
	}
	if s.emu.Emulator.IsAltScreen() {
		return true // a full-screen program's rows are not scrollback
	}
	s.integrated = true
	row := s.dropped + s.emu.Emulator.ScrollbackLen() + s.emu.Emulator.CursorPosition().Y
	switch mk.Kind {
	case 'A':
		if n := len(s.cmds); n > 0 && s.cmds[n-1].Row == row && !s.cmds[n-1].Done {
			return true // the same prompt redrawn
		}
		s.cmds = append(s.cmds, Command{Row: row})
	case 'D':
		if n := len(s.cmds); n > 0 && !s.cmds[n-1].Done {
			s.cmds[n-1].Done, s.cmds[n-1].Exit, s.cmds[n-1].HasExit = true, mk.Exit, mk.HasExit
		}
	}
	return true
}

func (s *Screen) onCwd(data []byte) bool {
	if p, ok := ParseCwd(data); ok {
		s.cwd = p
	}
	return true
}

// settle trims the scrollback back to its capacity after a chunk and
// keeps the absolute row base: lines dropped off the top are counted so
// marks keep their place.
func (s *Screen) settle(before int) {
	sb := s.emu.Scrollback()
	n := sb.Len()
	if n < before { // the program cleared the scrollback
		s.dropped += before - n
	}
	if n > s.capacity {
		sb.SetMaxLines(s.capacity)
		sb.SetMaxLines(s.capacity + scrollHeadroom)
		s.dropped += n - s.capacity
	}
	keep := s.cmds[:0]
	for _, c := range s.cmds {
		if c.Row >= s.dropped {
			keep = append(keep, c)
		}
	}
	s.cmds = keep
}

// Integrated reports that the shell has sent at least one prompt mark.
func (s *Screen) Integrated() bool { return s.integrated }

// Cwd is the working directory the shell last reported through OSC 7
// (empty until it does, or when it only reports remote paths).
func (s *Screen) Cwd() string { return s.cwd }

// Commands lists the marked prompts, oldest first.
func (s *Screen) Commands() []Command { return append([]Command(nil), s.cmds...) }

// RowBase is the absolute row of the first live screen row; the history
// row i (as in History) sits at RowBase-len(History)+i.
func (s *Screen) RowBase() int { return s.dropped + s.emu.ScrollbackLen() }
