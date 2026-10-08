package kit

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// TrnAuthor identifies who authored a transcript row. Human rows
// render in plain text, agent rows on the brand accent, system rows
// muted (F-026 P1e).
type TrnAuthor int

const (
	TrnHuman TrnAuthor = iota
	TrnAgent
	TrnSystem
)

// TrnRow is one transcript row. At drives the HH:MM stamp and the day
// divider (rows carry their own times — never the wall clock — so
// renders stay deterministic); Sep renders a separator banner above
// the row ("new messages"); Markdown routes the text through the
// injected Markdown seam (nil = raw text, never silent).
type TrnRow struct {
	Author   string
	Kind     TrnAuthor
	Text     string
	Thread   bool // threaded reply: ↳ prefix
	Markdown bool
	At       time.Time
	Sep      string
}

// Transcript renders channel/thread transcripts for every surface
// (workspace CHANNELS, editor chat, ideator CHAT, reviewer threads):
// day dividers, author-styled prefixes, timestamps, thread tags, and
// word-wrapped text at a shared text column. Pure render — scroll and
// cursors stay with the surface via Scroller.
type Transcript struct {
	Rows     []TrnRow
	Width    int
	CursorAt int                                     // TrnRow index carrying the cursor marker; -1 = none
	Tail     int                                     // visible lines; 0 = all; the window follows CursorAt
	Markdown func(markdown string, width int) string // injected seam
}

// authorW is the padded author-label width across the row set —
// thread tags ("↳ ") included, so thread rows keep the same column.
func (t *Transcript) authorW() int {
	w := 0
	for _, r := range t.Rows {
		label := r.Author
		if r.Thread {
			label = "↳ " + label
		}
		if n := runeWidth(label); n > w {
			w = n
		}
	}
	return clamp(w, 3, 40)
}

// textCol is the first text column: author + stamp + slack, budgeted
// so the cursor marker eats the slack — author labels never clip.
func (t *Transcript) textCol() int {
	col := t.authorW() + 8
	if t.Width > 0 && col > t.Width-8 {
		col = clamp(t.Width-8, 6, t.Width)
	}
	return col
}

// View renders the wrapped transcript lines. Empty row sets return nil
// so surfaces keep their own empty states.
func (t *Transcript) View() []string {
	if len(t.Rows) == 0 {
		return nil
	}
	col := t.textCol()
	textW := clamp(t.Width-col-2, 4, t.Width)
	var out []string
	firstLineOf := make([]int, len(t.Rows))
	prevDay := ""
	for ri, r := range t.Rows {
		marker := ""
		if ri == t.CursorAt {
			marker = theme.GlyphCursor + " "
		}
		if !r.At.IsZero() {
			day := r.At.Format("Mon Jan 2")
			if day != prevDay {
				out = append(out, theme.RailMuted().Render(
					padTo("── "+day+" ──", t.Width)))
				prevDay = day
			}
		}
		if r.Sep != "" {
			out = append(out, theme.RailMuted().Render(
				padTo("── "+r.Sep+" ──", t.Width)))
		}
		author := r.Author
		if r.Thread {
			author = "↳ " + author
		}
		stamp := ""
		if !r.At.IsZero() {
			stamp = r.At.Format("15:04")
		}
		// The label clips into the fixed prefix budget (thread tags
		// included) so every row's text starts at the same column; the
		// cursor marker eats two cells of the label budget so the row
		// keeps the shared column.
		labelW := col - 6
		if marker != "" {
			labelW = col - 8
		}
		label := ClipEllipsis(author, clamp(labelW, 1, col-6))
		prefix := marker + authorStyle(r.Kind).Render(padTo(label, labelW)) +
			theme.TextMuted().Render(" "+stamp)
		indent := strings.Repeat(" ", col+2)
		firstLineOf[ri] = len(out)
		for i, tl := range t.textLines(r, textW) {
			if i == 0 {
				out = append(out, prefix+"  "+tl)
				continue
			}
			out = append(out, indent+tl)
		}
	}

	// Tail window: chats show the latest (F-026 P3). The window follows
	// the cursor row's first line so `k`-nav into older messages keeps
	// its target visible — a cursor can never vanish into the clipped
	// block invisibly.
	if t.Tail > 0 && t.Tail < len(out) {
		from := clamp(len(out)-t.Tail, 0, len(out)-1)
		if t.CursorAt >= 0 && t.CursorAt < len(firstLineOf) {
			cur := clamp(firstLineOf[t.CursorAt], 0, len(out)-1)
			if cur < from {
				from = cur
			}
			if cur >= from+t.Tail {
				from = cur - t.Tail + 1
			}
		}
		to := clamp(from+t.Tail, 0, len(out))
		out = out[from:to]
	}
	return out
}

// textLines renders one row's text: markdown through the seam (block
// content keeps its line structure), word-wrapped prose otherwise.
func (t *Transcript) textLines(r TrnRow, textW int) []string {
	text := r.Text
	if r.Markdown && t.Markdown != nil {
		if rendered := t.Markdown(text, textW); rendered != "" {
			text = rendered
		}
	}
	// Bodies read in the primary text color; who spoke is carried by the
	// author label's color (F-055) — an all-accent transcript is hard to
	// read and leaves the accent meaning nothing.
	st := theme.TextStyle()
	if r.Kind == TrnSystem {
		st = theme.TextMuted()
	}
	if strings.TrimSpace(text) == "" {
		return []string{""}
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.TrimSpace(strings.ReplaceAll(l, " ", "")) == "" {
			out = append(out, st.Render(strings.Repeat(" ", textW)))
			continue
		}
		for _, w := range WrapWords(l, textW) {
			if w == "" {
				continue
			}
			out = append(out, st.Render(padTo(w, textW)))
		}
	}
	return out
}

// WrapWords breaks text at spaces to fit width, hard-breaking tokens
// longer than width (the F-016 contract, one shared implementation).
func WrapWords(text string, width int) []string {
	width = clamp(width, 1, 4096)
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			if runeWidth(w) > width {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				rs := []rune(w)
				tw := 0
				start := 0
				for i, r := range rs {
					rw := runewidth.RuneWidth(r)
					if tw+rw > width && i > start {
						out = append(out, string(rs[start:i]))
						start = i
						tw = 0
					}
					tw += rw
				}
				out = append(out, string(rs[start:]))
				continue
			}
			if line == "" {
				line = w
			} else if runeWidth(line)+1+runeWidth(w) <= width {
				line += " " + w
			} else {
				out = append(out, line)
				line = w
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// authorStyle colors a transcript author label by who spoke: you in the
// brand accent, agents in the secondary accent, system lines muted.
func authorStyle(k TrnAuthor) lipgloss.Style {
	switch k {
	case TrnHuman:
		return theme.AccentText()
	case TrnAgent:
		return lipgloss.NewStyle().Foreground(theme.Current.Accent2)
	}
	return theme.TextMuted()
}
