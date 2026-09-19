package editor

// syntax.go colorizes editor buffers (F-026 P4, ADR-0015). Chroma
// tokenizes the whole buffer so multi-line lexer state survives; token
// kinds map to theme tokens — never chroma style hexes — so dark/light
// re-theming keeps its single source of truth. No chroma import exists
// outside this file. The whole-buffer lex caches on the buffer's
// mutation sequence (textbuf.Buffer.Seq); cursor/selection lines render
// plain so the rune-level inversion stays exact.

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// syntaxMaxSize gates colorization for pathological files (the lex is
// whole-buffer, per edit — a few hundred KB stays responsive).
const syntaxMaxSize = 256 << 10

// highlighter caches one buffer's styled lines between edits.
type highlighter struct {
	path  string
	seq   uint64
	done  bool // seq 0 is a valid cache state — done distinguishes fresh
	lines []string
}

// styled reports the colored render for line i ("" = no styling).
func (h *highlighter) styled(i int) string {
	if i < 0 || i >= len(h.lines) {
		return ""
	}
	return h.lines[i]
}

// refresh re-lexes when the buffer moved on from the cached content.
// A nil result stays nil-safe: unlexable buffers render plain, never
// fake colored (F-011).
func (h *highlighter) refresh(b *textbuf.Buffer) {
	if b == nil {
		return
	}
	if h.done && h.seq == b.Seq() {
		return
	}
	h.done = true
	h.seq = b.Seq()
	text := b.Text()
	h.lines = make([]string, b.LineCount())
	lexer := lexers.Match(h.path)
	if lexer == nil || len(text) == 0 || len(text) > syntaxMaxSize {
		return
	}
	iter, err := lexer.Tokenise(nil, text)
	if err != nil {
		return
	}
	cur := 0
	var sb strings.Builder
	flush := func() {
		if cur < len(h.lines) {
			h.lines[cur] = sb.String()
		}
		cur++
		sb.Reset()
	}
	for tok := iter(); tok != chroma.EOF; tok = iter() {
		val := tok.Value
		st, styled := tokenStyle(tok.Type)
		for {
			if i := strings.IndexByte(val, '\n'); i >= 0 {
				sb.WriteString(styledPart(st, styled, val[:i]))
				flush()
				val = val[i+1:]
				continue
			}
			sb.WriteString(styledPart(st, styled, val))
			break
		}
	}
	flush()
}

// styledPart renders one token fragment in its mapped color or plain.
func styledPart(st lipgloss.Style, styled bool, text string) string {
	if !styled {
		return text
	}
	return st.Render(text)
}

// tokenStyle maps chroma token kinds onto the DHI token set (ADR-0015:
// kinds, never chroma style hexes — the theme owns every color). ok
// false renders the fragment plain.
func tokenStyle(t chroma.TokenType) (lipgloss.Style, bool) {
	switch {
	case t.InCategory(chroma.Comment):
		return theme.TextMuted(), true
	case t.InCategory(chroma.Keyword):
		return theme.Accent2Bold(), true
	case t.InCategory(chroma.String):
		return theme.SuccessText(), true
	case t.InCategory(chroma.LiteralNumber):
		return theme.InfoText(), true
	case t.InCategory(chroma.NameFunction), t.InCategory(chroma.NameClass):
		return theme.AccentText(), true
	case t.InCategory(chroma.NameTag), t.InCategory(chroma.NameAttribute):
		return theme.Accent2Bold(), true
	case t.InCategory(chroma.Operator), t.InCategory(chroma.Punctuation):
		return theme.AccentDimText(), true
	}
	return lipgloss.Style{}, false
}
