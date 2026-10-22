package kit

import tea "charm.land/bubbletea/v2"

// System clipboard (F-103, ADR-0042). Copy always emits OSC 52, which
// iTerm2, Ghostty, kitty, WezTerm, Alacritty and tmux put on the system
// clipboard (over SSH too), and also calls the OS clipboard tool that
// cmd injects (pbcopy, wl-copy, xclip) for terminals that ignore OSC 52,
// such as macOS Terminal.app. kit never shells out itself.

var osClipboard func(string) error

// SetOSClipboard installs the OS clipboard writer (nil: OSC 52 only).
func SetOSClipboard(f func(string) error) { osClipboard = f }

// CopiedMsg reports a finished copy; Err is the OS tool's error, if any
// (OSC 52 has no acknowledgement).
type CopiedMsg struct {
	Chars int
	Err   error
}

// Copy puts text on the system clipboard.
func Copy(text string) tea.Cmd {
	n := len([]rune(text))
	osCopy := func() tea.Msg {
		if osClipboard == nil {
			return CopiedMsg{Chars: n}
		}
		return CopiedMsg{Chars: n, Err: osClipboard(text)}
	}
	return tea.Batch(tea.SetClipboard(text), osCopy)
}
