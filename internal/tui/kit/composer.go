package kit

import "github.com/drjzlyan/dhi/internal/tui/theme"

// Composer renders the one-line message input every chat surface shares
// (F-093): a prompt chevron, the typed text and a cursor while focused;
// a muted placeholder when empty. Its send/mention keys live on the
// shell status row (ADR-0040), never on a second hint line.
func Composer(in Input, placeholder string, focused bool) string {
	if !focused {
		if in.Value != "" {
			return theme.TextMuted().Render("› ") + theme.TextDim().Render(in.Value)
		}
		return theme.TextMuted().Render("› " + placeholder)
	}
	prompt := theme.AccentText().Render("› ")
	cursor := theme.AccentText().Render("▌")
	if in.Value == "" {
		return prompt + cursor + theme.TextMuted().Render(placeholder)
	}
	rs := []rune(in.Value)
	cur := in.Cursor()
	return prompt + theme.TextStyle().Render(string(rs[:cur])) + cursor + theme.TextStyle().Render(string(rs[cur:]))
}
