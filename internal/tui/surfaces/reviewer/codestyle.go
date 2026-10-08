package reviewer

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/tui/syntax"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// lineStyle is what the diff knows about one line's text: a syntax class
// per rune and, for a paired changed line, which runes changed (F-049).
type lineStyle struct {
	classes []syntax.Class
	marks   []bool
}

// styleKey addresses a line for style lookup: context by its new number,
// changed lines as gitdiff.KeyOf does.
func styleKey(l gitdiff.Line) gitdiff.MarkKey {
	if l.Kind == gitdiff.Ctx {
		return gitdiff.MarkKey{Kind: gitdiff.Ctx, No: l.NewNo}
	}
	return gitdiff.KeyOf(l)
}

// fileStyles computes (once per diff) the styles of every line of file fi.
// Syntax is lexed per hunk side — the old side from context + removed
// lines, the new side from context + added lines — so multi-line tokens
// inside a hunk survive. Unknown languages and huge hunks stay plain.
func (m *Model) fileStyles(fi int) map[gitdiff.MarkKey]lineStyle {
	if m.styleCache == nil {
		m.styleCache = map[int]map[gitdiff.MarkKey]lineStyle{}
	}
	if st, ok := m.styleCache[fi]; ok {
		return st
	}
	out := map[gitdiff.MarkKey]lineStyle{}
	m.styleCache[fi] = out
	if fi < 0 || fi >= len(m.files) {
		return out
	}
	f := m.files[fi]
	path := f.DisplayPath()
	for _, h := range f.Hunks {
		var oldLines, newLines []gitdiff.Line
		for _, l := range h.Lines {
			switch l.Kind {
			case gitdiff.Del:
				oldLines = append(oldLines, l)
			case gitdiff.Add:
				newLines = append(newLines, l)
			default:
				oldLines = append(oldLines, l)
				newLines = append(newLines, l)
			}
		}
		assign := func(lines []gitdiff.Line, skipCtx bool) {
			texts := make([]string, len(lines))
			for i, l := range lines {
				texts[i] = l.Text
			}
			lexed := syntax.Lex(path, strings.Join(texts, "\n"))
			for i, l := range lines {
				if skipCtx && l.Kind == gitdiff.Ctx {
					continue
				}
				ls := out[styleKey(l)]
				if i < len(lexed) {
					ls.classes = lexed[i].Classes()
				}
				out[styleKey(l)] = ls
			}
		}
		assign(newLines, false)
		assign(oldLines, true) // context already came from the new side
		for key, marks := range gitdiff.WordMarks(h) {
			ls := out[key]
			ls.marks = marks
			out[key] = ls
		}
	}
	return out
}

func (m *Model) styleOf(fi int, l *gitdiff.Line) lineStyle {
	if l == nil {
		return lineStyle{}
	}
	return m.fileStyles(fi)[styleKey(*l)]
}

// runeCell is one display cell of a code row.
type runeCell struct {
	r     rune
	class syntax.Class
	mark  bool
}

// expandCells turns a line into cells, tabs becoming four spaces (terminals
// disagree on tab stops; fixed here as wrapPlain always did).
func expandCells(text string, ls lineStyle) []runeCell {
	var cells []runeCell
	for i, r := range []rune(text) {
		c := runeCell{r: r}
		if i < len(ls.classes) {
			c.class = ls.classes[i]
		}
		if i < len(ls.marks) {
			c.mark = ls.marks[i]
		}
		if r == '\t' {
			for k := 0; k < 4; k++ {
				cells = append(cells, runeCell{r: ' ', class: c.class, mark: c.mark})
			}
			continue
		}
		cells = append(cells, c)
	}
	return cells
}

// codeStyle is the style of one run: syntax colour over the line's wash;
// inside a changed word the plain text colour on the stronger wash, so the
// highlight never costs legibility (the theme tests pin this).
func codeStyle(kind gitdiff.Kind, class syntax.Class, mark bool) lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(theme.Current.Text)
	if cs, ok := class.Style(); ok && !mark {
		st = st.Foreground(cs.GetForeground()).Bold(cs.GetBold())
	}
	switch kind {
	case gitdiff.Add:
		if mark {
			return st.Background(theme.Current.BgAddStrong).Bold(true)
		}
		return st.Background(theme.Current.BgAdd)
	case gitdiff.Del:
		if mark {
			return st.Background(theme.Current.BgDelStrong).Bold(true)
		}
		return st.Background(theme.Current.BgDel)
	}
	return st
}

// codeRows renders a line's code wrapped to width, every row padded to
// exactly width columns (added/removed rows fill with their wash).
func codeRows(text string, ls lineStyle, kind gitdiff.Kind, width int) []string {
	width = maxInt(width, 4)
	cells := expandCells(text, ls)
	if len(cells) == 0 {
		return []string{padCode("", kind, width)}
	}
	var rows []string
	for start := 0; start < len(cells); start += width {
		chunk := cells[start:minInt(start+width, len(cells))]
		var sb strings.Builder
		for i := 0; i < len(chunk); {
			j := i + 1
			for j < len(chunk) && chunk[j].class == chunk[i].class && chunk[j].mark == chunk[i].mark {
				j++
			}
			run := make([]rune, 0, j-i)
			for _, c := range chunk[i:j] {
				run = append(run, c.r)
			}
			sb.WriteString(codeStyle(kind, chunk[i].class, chunk[i].mark).Render(string(run)))
			i = j
		}
		rows = append(rows, sb.String()+padCode("", kind, width-len(chunk)))
	}
	return rows
}

// padCode is n columns of the line's background (blank for context).
func padCode(_ string, kind gitdiff.Kind, n int) string {
	if n <= 0 {
		return ""
	}
	pad := strings.Repeat(" ", n)
	switch kind {
	case gitdiff.Add:
		return lipgloss.NewStyle().Background(theme.Current.BgAdd).Render(pad)
	case gitdiff.Del:
		return lipgloss.NewStyle().Background(theme.Current.BgDel).Render(pad)
	}
	return pad
}
