package editor

import (
	"os"
	"strings"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Terminal shell integration (F-157). A shell that already emits OSC 133
// prompt marks and OSC 7 working-directory reports (fish, starship,
// iTerm2/WezTerm/VS Code integration, recent zsh/bash setups) gives the
// drawer: alt+k / alt+j to jump between command prompts in the
// scrollback, a ✗ at the right edge of a prompt whose command failed,
// new tabs and splits opened in the shell's current folder, and
// "send to terminal" from the editor. DHI never edits a shell rc file
// (ADR-0044); without the marks these stay idle and say how to enable
// them.

// integrationHint is what a toast tells a user whose shell sends no marks.
const integrationHint = "this shell sends no prompt marks (OSC 133) · enable your shell's or prompt tool's integration (fish and starship have it; see docs/features/F-157)"

// workDir is the folder new tabs and splits open in: where the shell
// last said it is (OSC 7), else the folder the tab was started in.
// A reported folder that no longer exists falls back to the tab's own
// (the shell is gone from it; term.Start would refuse it).
func (t *termTab) workDir() string {
	if t.screen != nil {
		if c := t.screen.Cwd(); c != "" {
			if fi, err := os.Stat(c); err == nil && fi.IsDir() {
				return c
			}
		}
	}
	return t.dir
}

// markFailed draws a ✗ at the right edge of every visible prompt whose
// command exited non-zero. lines[0] is the absolute row absTop.
func markFailed(t *termTab, lines []string, absTop, cols int) {
	if t.screen == nil || t.screen.AltScreen() || cols < 2 {
		return
	}
	glyph := theme.DangerText().Render(theme.GlyphCross)
	for _, c := range t.screen.Commands() {
		i := c.Row - absTop
		if !c.Failed() || i < 0 || i >= len(lines) {
			continue
		}
		lines[i] = padCells(lines[i], cols-1) + "\x1b[0m" + glyph
	}
}

// jumpPrompt scrolls the active terminal to the previous (dir -1) or
// next (dir +1) command prompt, putting it on the top row.
func (m *Model) jumpPrompt(dir int) {
	t := m.activeTermTab()
	if t == nil {
		return
	}
	cmds := t.screen.Commands()
	if len(cmds) == 0 {
		m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Terminal", Detail: integrationHint}
		return
	}
	base := t.screen.RowBase()
	top := base - t.scroll // absolute row at the top of the view
	ref := top
	switch {
	case t.hasJump: // keep walking from the prompt the last jump chose
		ref = t.jumpRow
	case t.scroll == 0:
		ref = max(top, cmds[len(cmds)-1].Row) // live: "previous" is before the current prompt
	}
	target := -1
	if dir < 0 {
		for _, c := range cmds {
			if c.Row < ref {
				target = c.Row
			}
		}
		if target < 0 {
			m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Terminal", Detail: "no earlier prompt in the scrollback"}
			return
		}
	} else {
		if t.scroll == 0 && !t.hasJump {
			return
		}
		for _, c := range cmds {
			if c.Row > ref {
				target = c.Row
				break
			}
		}
		if target < 0 {
			t.scroll, t.hasJump = 0, false
			return
		}
	}
	t.jumpRow, t.hasJump = target, true
	t.scroll = max(base-target, 0)
}

// sendCommand handles :send — the visual selection (or the cursor line)
// goes to the active terminal as a paste.
func (m *Model) sendCommand(requester *textbuf.Editor, cmd string) (string, bool) {
	if strings.TrimSpace(cmd) != "send" || requester == nil {
		return "", false
	}
	text := ""
	if sel, ok := requester.TakeSelection(); ok {
		text = sel.Text
	} else {
		text = requester.Buffer().Line(requester.Buffer().Cursor().Line)
	}
	return m.sendToTerm(text), true
}

// sendToTerm pastes text into the active terminal tab and focuses it.
// It adds no newline: the user reads it in the prompt and presses enter.
func (m *Model) sendToTerm(text string) string {
	if text == "" {
		return "nothing to send"
	}
	m.drawerOpen = true
	m.ensureTermTabs()
	t := m.activeTermTab()
	if t == nil || t.sess == nil || t.exited {
		return "no live terminal · open one with ctrl+t (alt+r restarts an exited tab)"
	}
	t.screen.Paste(text) // bracketed when the shell asked for it
	t.scroll = 0
	m.termFocus = true
	m.syncTermSize()
	return "sent to the terminal · press enter there to run it"
}
