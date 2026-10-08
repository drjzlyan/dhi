// Package syntax tokenizes source text into classed spans for the editor
// and the review diff (ADR-0015, F-049). Chroma sees the whole text so
// multi-line lexer state survives; token kinds map to a small CLASS set and
// classes map to theme colours at render time — never chroma style hexes —
// so dark/light re-theming keeps one source of truth. This is the only
// package that imports chroma.
package syntax

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// MaxSize gates tokenizing for pathological inputs (the lex is whole-text).
const MaxSize = 256 << 10

// Class is a coarse token kind.
type Class uint8

// Token classes.
const (
	Plain Class = iota
	Comment
	Keyword
	String
	Number
	Func
	Tag
	Operator
)

// Span is a run of text of one class.
type Span struct {
	Text  string
	Class Class
}

// Line is one source line as spans (no newline).
type Line []Span

// Text is the line's plain text.
func (l Line) Text() string {
	var sb strings.Builder
	for _, s := range l {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// Classes expands the line to one class per rune.
func (l Line) Classes() []Class {
	var out []Class
	for _, s := range l {
		for range s.Text {
			out = append(out, s.Class)
		}
	}
	return out
}

func classOf(t chroma.TokenType) Class {
	// Sub-category checks matter: chroma's InCategory matches the WHOLE
	// category, so InCategory(String) is true for every literal (numbers
	// included) and InCategory(NameFunction) for every identifier.
	switch {
	case t.InCategory(chroma.Comment):
		return Comment
	case t.InCategory(chroma.Keyword):
		return Keyword
	case t.InSubCategory(chroma.LiteralString):
		return String
	case t.InSubCategory(chroma.LiteralNumber):
		return Number
	// Name tokens are flat constants (NameOther and NameAttribute share a
	// sub-category), so these are exact matches.
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic || t == chroma.NameClass:
		return Func
	case t == chroma.NameTag || t == chroma.NameAttribute:
		return Tag
	case t.InCategory(chroma.Operator), t.InCategory(chroma.Punctuation):
		return Operator
	}
	return Plain
}

// Style maps a class onto the DHI colour set; ok is false for Plain.
func (c Class) Style() (lipgloss.Style, bool) {
	switch c {
	case Comment:
		return theme.TextMuted(), true
	case Keyword:
		return theme.Accent2Bold(), true
	case String:
		return theme.SuccessText(), true
	case Number:
		return theme.InfoText(), true
	case Func:
		return theme.AccentText(), true
	case Tag:
		return theme.Accent2Bold(), true
	case Operator:
		return theme.AccentDimText(), true
	}
	return lipgloss.Style{}, false
}

// Lex tokenizes text for the file at path. It returns one Line per
// "\n"-separated source line, or nil when the language is unknown, the text
// is empty or too large, or lexing fails — callers then render plain,
// never fake-coloured (F-011).
func Lex(path, text string) []Line {
	lexer := lexers.Match(path)
	if lexer == nil || len(text) == 0 || len(text) > MaxSize {
		return nil
	}
	iter, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil
	}
	lines := []Line{nil}
	for tok := iter(); tok != chroma.EOF; tok = iter() {
		c := classOf(tok.Type)
		val := tok.Value
		for {
			i := strings.IndexByte(val, '\n')
			if i < 0 {
				if val != "" {
					lines[len(lines)-1] = append(lines[len(lines)-1], Span{Text: val, Class: c})
				}
				break
			}
			if i > 0 {
				lines[len(lines)-1] = append(lines[len(lines)-1], Span{Text: val[:i], Class: c})
			}
			lines = append(lines, nil)
			val = val[i+1:]
		}
	}
	return lines
}

// Render paints a line in its class colours (unstyled spans stay plain).
func Render(l Line) string {
	var sb strings.Builder
	for _, s := range l {
		if st, ok := s.Class.Style(); ok {
			sb.WriteString(st.Render(s.Text))
		} else {
			sb.WriteString(s.Text)
		}
	}
	return sb.String()
}
