// Package bootgate renders a blocked boot (F-011 / ADR-0011, amended by
// ADR-0044): a named reason and fixes, never released. Boot blocks only
// on a broken workspace or settings file; missing tools never block — the
// capability that needs one raises its install card (F-110).
package bootgate

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/boot"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Model is the boot gate.
type Model struct {
	version       string
	decision      boot.Decision
	width, height int
}

// Compile-time check against the shell's gate contract.
var _ gate = (*Model)(nil)

// gate mirrors app.Gate (structurally identical); kept local so the
// gate surfaces never import the shell.
type gate interface {
	Init() tea.Cmd
	Resize(width, height int)
	Update(tea.Msg) tea.Cmd
	HandleKey(key string) bool
	View() string
	Finished() bool
}

// New builds the gate from the audit decision: a blocked decision never
// finishes; anything else is done at once (the shell releases).
func New(version string, d boot.Decision) *Model {
	return &Model{version: version, decision: d}
}

func (m *Model) Resize(w, h int) { m.width, m.height = w, h }

func (m *Model) Init() tea.Cmd { return nil }

// Finished reports whether the shell may release. A blocked boot never
// does — quitting is the only exit.
func (m *Model) Finished() bool { return m.decision.Block == "" }

func (m *Model) Update(tea.Msg) tea.Cmd { return nil }

// HandleKey swallows every key (ctrl+c/ctrl+q quit at the shell).
func (m *Model) HandleKey(string) bool { return true }

func (m *Model) View() string {
	if m.decision.Block == "" {
		return ""
	}
	return m.blockView()
}

func (m *Model) blockView() string {
	w := m.cardTextWidth()
	var lines []string
	push := func(s string) { lines = append(lines, wrapLine(s, w)...) }
	push(theme.DangerText().Render("boot blocked — no silent fallbacks (ADR-0011)"))
	lines = append(lines, "")
	push(m.decision.Block)
	lines = append(lines, "", "fixes:")
	for _, f := range m.decision.Fixes {
		push("  · " + f)
	}
	lines = append(lines, "",
		theme.Hint().Render("ctrl+q quit · dhi doctor diagnoses a blocked boot"))
	return m.card(lines)
}

// cardWidth is the boot card's width: a readable column (F-054), never
// the whole terminal — names and sizes stay close enough to read as rows.
func (m *Model) cardWidth() int { return min(max(m.width-4, 30), 80) }

// cardTextWidth is the text column inside the card's border and padding.
func (m *Model) cardTextWidth() int { return m.cardWidth() - 6 }

// card frames lines in a panel sized to its content, centered on screen.
func (m *Model) card(lines []string) string {
	p := kit.NewPanel("dhi "+m.version, false)
	p.Boxed = true // gates keep their frame (ADR-0040)
	p.SetContent(lines...)
	p.Width = m.cardWidth()
	p.Height = min(len(lines)+2, max(m.height, 8))
	return kit.Center(p.View(), max(m.width, p.Width), max(m.height, p.Height))
}

// wrapLine word-wraps plain text to width; styles are inserted only on
// uniform-color lines (hint/danger/warn), so plain wrapWidth math holds.
func wrapLine(s string, width int) []string {
	if width <= 0 || len(s) <= width {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	for _, word := range strings.Fields(s) {
		need := len(word)
		if cur.Len() > 0 {
			need += 1 + cur.Len()
		}
		if need <= width && cur.Len() > 0 {
			cur.WriteByte(' ')
			cur.WriteString(word)
			continue
		}
		if len(word) > width {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			for len(word) > width {
				out = append(out, word[:width])
				word = word[width:]
			}
			cur.WriteString(word)
			continue
		}
		if cur.Len() > 0 { // never emit an empty leading line
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
