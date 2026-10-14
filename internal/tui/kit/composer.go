package kit

import "github.com/drjzlyan/dhi/internal/tui/theme"

// Composer renders the one-line message input every chat surface shares
// (F-093): a prompt chevron, the typed text and a cursor while focused;
// a muted placeholder when empty. Its send/mention keys live on the
// shell status row (ADR-0040), never on a second hint line.
func Composer(input, placeholder string, focused bool) string {
	if !focused {
		if input != "" {
			return theme.TextMuted().Render("› ") + theme.TextDim().Render(input)
		}
		return theme.TextMuted().Render("› " + placeholder)
	}
	prompt := theme.AccentText().Render("› ")
	cursor := theme.AccentText().Render("▌")
	if input == "" {
		return prompt + cursor + theme.TextMuted().Render(placeholder)
	}
	return prompt + theme.TextStyle().Render(input) + cursor
}
