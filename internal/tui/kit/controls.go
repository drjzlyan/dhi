package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Checkbox draws "✓ label" or "○ label" (F-096): the state is a glyph,
// never color alone. Focused rows put the label in the focus hue.
func Checkbox(label string, on, focused bool) string {
	mark := theme.TextMuted().Render(theme.GlyphTodo)
	if on {
		mark = theme.SuccessText().Render(theme.GlyphCheck)
	}
	return mark + " " + controlLabel(label, focused)
}

// Radio draws "● label" for the chosen option and "○ label" otherwise.
func Radio(label string, on, focused bool) string {
	mark := theme.TextMuted().Render(theme.GlyphTodo)
	if on {
		mark = theme.AccentText().Render(theme.GlyphDot)
	}
	return mark + " " + controlLabel(label, focused)
}

// Toggle draws a cycling choice as "‹choice›": the chevrons say ←/→
// change it. Focused, it sits on the input well with the focus edge, the
// same control language as Input.
func Toggle(choice string, focused bool) string {
	if focused {
		return theme.Input().Bold(true).Render("‹" + choice + "›")
	}
	return theme.TextDim().Render("‹" + choice + "›")
}

func controlLabel(label string, focused bool) string {
	if focused {
		return theme.AccentText().Render(label)
	}
	return theme.TextStyle().Render(label)
}

// badgeGlyphs pair each badge kind with a glyph so meaning survives
// without color (WCAG 1.4.1).
var badgeGlyphs = map[theme.BadgeKind]string{
	theme.BadgeOK:      theme.GlyphCheck,
	theme.BadgeWarn:    "!",
	theme.BadgeError:   theme.GlyphCross,
	theme.BadgeInfo:    "i",
	theme.BadgeBusy:    theme.GlyphBusy,
	theme.BadgeNeutral: "·",
}

// Badge draws a status pill: " ✓ installed ".
func Badge(kind theme.BadgeKind, text string) string {
	return theme.Badge(kind).Render(" " + badgeGlyphs[kind] + " " + text + " ")
}

// BadgeWidth is Badge's width in cells.
func BadgeWidth(text string) int { return runeWidth(text) + 4 }

// Card is a titled block (F-096): a stripe down the left (focus hue when
// selected, secondary otherwise), a bold title with an optional badge on
// the right, wrapped body text and an optional row of buttons.
type Card struct {
	Title     string
	Body      string
	Badge     string
	BadgeKind theme.BadgeKind
	Buttons   []Button
	Selected  bool
}

// View renders the card w cells wide; spans locate its buttons (Line is
// relative to the card's first row, columns include the stripe).
func (c Card) View(w int) ([]string, []ButtonSpan) {
	w = max(w, 8)
	inner := w - 1 // stripe
	stripe := theme.CardStripe(c.Selected).Render("▌")
	bg := theme.CardBg()
	var out []string
	title := " " + c.Title
	badge := ""
	if c.Badge != "" {
		badge = Badge(c.BadgeKind, c.Badge)
		title = ClipEllipsis(title, inner-BadgeWidth(c.Badge)-1)
	} else {
		title = ClipEllipsis(title, inner)
	}
	gap := inner - runeWidth(title)
	if badge != "" {
		gap -= BadgeWidth(c.Badge)
	}
	row := theme.CardTitle(c.Selected).Render(title) + bg.Render(strings.Repeat(" ", max(gap, 0))) + badge
	out = append(out, stripe+row)
	if c.Body != "" {
		for _, para := range strings.Split(c.Body, "\n") {
			for _, l := range WrapWords(para, inner-2) {
				out = append(out, stripe+theme.CardText().Render(" "+l+strings.Repeat(" ", max(inner-1-runeWidth(l), 0))))
			}
		}
	}
	var spans []ButtonSpan
	if len(c.Buttons) > 0 {
		lines, bs := Buttons(c.Buttons, inner-2)
		base := len(out)
		for _, l := range lines {
			out = append(out, stripe+bg.Render(" ")+Restyle(bg, l)+bg.Render(strings.Repeat(" ", max(inner-1-runeWidth(l), 0))))
		}
		for _, s := range bs {
			s.Line += base
			s.From += 2
			s.To += 2
			spans = append(spans, s)
		}
	}
	return out, spans
}

// ChecklistState is where a checklist item stands.
type ChecklistState int

const (
	ItemPending ChecklistState = iota
	ItemDone
	ItemSkipped
)

// ChecklistItem is one row of a Checklist.
type ChecklistItem struct {
	ID    string
	Title string
	Why   string // one line: what this gives you
	State ChecklistState
}

// Checklist is a guided list of things to do (F-098): each row is a
// state glyph, the title and its why; the cursor row is highlighted and
// shows the action key.
type Checklist struct {
	Items  []ChecklistItem
	Cursor int
	Focus  bool // draw the cursor row (the list has keyboard focus)
}

// Done counts finished items (skipped ones are not done).
func (c Checklist) Done() int {
	n := 0
	for _, it := range c.Items {
		if it.State == ItemDone {
			n++
		}
	}
	return n
}

// Settled counts items that are done or skipped.
func (c Checklist) Settled() int {
	n := 0
	for _, it := range c.Items {
		if it.State != ItemPending {
			n++
		}
	}
	return n
}

// View renders one row per item, w cells wide.
func (c Checklist) View(w int) []string {
	out := make([]string, 0, len(c.Items))
	for i, it := range c.Items {
		sel := c.Focus && i == c.Cursor
		var mark string
		switch it.State {
		case ItemDone:
			mark = theme.SuccessText().Render(theme.GlyphCheck)
		case ItemSkipped:
			mark = theme.TextMuted().Render("–")
		default:
			mark = theme.AccentText().Render(theme.GlyphTodo)
		}
		title := it.Title
		titleSt := theme.TextStyle()
		switch {
		case sel:
			titleSt = theme.AccentText()
		case it.State != ItemPending:
			titleSt = theme.TextDim()
		}
		why := ""
		if it.Why != "" {
			why = "  " + it.Why
		}
		action := ""
		if sel {
			switch it.State {
			case ItemPending:
				action = "  ⏎ start"
			default:
				action = "  ⏎ redo"
			}
		}
		room := w - 2 - runeWidth(title) - runeWidth(action)
		why = ClipEllipsis(why, max(room, 0))
		row := mark + " " + titleSt.Render(title) + theme.TextMuted().Render(why)
		if action != "" {
			row += strings.Repeat(" ", max(room-runeWidth(why), 0)) + theme.AccentText().Render(action)
		}
		if sel {
			row = Restyle(theme.SelectionBg(), row)
			row += theme.SelectionBg().Render(strings.Repeat(" ", max(w-runeWidth(row), 0)))
		}
		out = append(out, row)
	}
	return out
}
