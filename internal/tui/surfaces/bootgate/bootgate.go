// Package bootgate renders the boot decision (F-011 / ADR-0011): a
// blocked boot (named reason + fixes, never released), or the
// confirmation-gated install of missing hermetic pieces (delegate to
// the bootstrap surface, then a source-build step for gopls). Skipping
// the install releases the shell — every declined capability then
// refuses at use with the named fix and doctor fails its row.
package bootgate

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/boot"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/bootstrap"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

type phase uint8

const (
	phaseBlocked phase = iota
	phaseConfirm
	phaseInstalling
	phaseBuilding
	phaseDone
)

// Model is the boot gate state machine.
type Model struct {
	version       string
	decision      boot.Decision
	width, height int
	phase         phase

	inner    *bootstrap.Model // owns the manifest install phase
	mgr      *toolchain.Manager
	buildErr string
	pending  tea.Cmd // command queued by HandleKey (confirm → install)
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

// New builds the gate from the audit decision. A blocked decision never
// finishes; an offer shows the confirmation list; a clean decision is
// immediately done (the shell releases at once).
func New(version string, d boot.Decision, mgr *toolchain.Manager) *Model {
	m := &Model{version: version, decision: d, mgr: mgr}
	switch {
	case d.Block != "":
		m.phase = phaseBlocked
	case len(d.Offer) > 0:
		m.phase = phaseConfirm
	default:
		m.phase = phaseDone
	}
	return m
}

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
	if m.inner != nil {
		m.inner.Resize(w, h)
	}
}

func (m *Model) Init() tea.Cmd {
	if m.phase == phaseInstalling {
		return m.inner.Init()
	}
	return nil
}

// Finished reports whether the shell may release. A blocked boot never
// does — quitting is the only exit.
func (m *Model) Finished() bool { return m.phase == phaseDone }

type buildDoneMsg struct{ err error }

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case buildDoneMsg:
		if msg.err != nil {
			m.buildErr = msg.err.Error()
		}
		m.phase = phaseDone
		return nil
	}
	if m.phase == phaseInstalling && m.inner != nil {
		cmd := m.inner.Update(msg)
		if m.inner.Finished() {
			return tea.Batch(cmd, m.afterInstall())
		}
		return cmd
	}
	return nil
}

// HandleKey consumes confirm/skip keys in the confirm phase; the block
// screen swallows everything (ctrl+c/ctrl+q quit at the shell).
func (m *Model) HandleKey(key string) bool {
	switch m.phase {
	case phaseConfirm:
		switch key {
		case "i", "I":
			m.startInstall()
		case "enter", "esc", "q":
			m.phase = phaseDone // skip: capabilities refuse at use
		}
	case phaseBuilding:
		if m.buildErr != "" {
			switch key {
			case "enter", "esc", "q":
				m.phase = phaseDone // refuse-at-use for LSP
			}
		}
	}
	return true
}

// TakeCmd drains a command queued by HandleKey (the shell returns it
// right after the key, since gates cannot return commands from key
// handling — without this drain the delegated install never starts).
func (m *Model) TakeCmd() tea.Cmd {
	cmd := m.pending
	m.pending = nil
	return cmd
}

func (m *Model) startInstall() {
	m.inner = bootstrap.New(m.version, m.mgr, "")
	m.inner.Resize(m.width, m.height)
	m.phase = phaseInstalling
	m.pending = m.inner.Init() // start install + event pump + tick
}

// afterInstall builds gopls from source when the manifest install did
// not provide it and the go shim exists to do the build.
func (m *Model) afterInstall() tea.Cmd {
	if m.mgr == nil {
		m.phase = phaseDone
		return nil
	}
	if _, err := os.Stat(filepath.Join(m.mgr.ShimDir(), "gopls")); err == nil {
		m.phase = phaseDone
		return nil
	}
	if _, err := os.Stat(filepath.Join(m.mgr.ShimDir(), "go")); err != nil {
		m.buildErr = "gopls needs the go toolchain (install it via bootstrap)"
		m.phase = phaseDone
		return nil
	}
	m.phase = phaseBuilding
	return func() tea.Msg {
		err := m.mgr.BuildInstall(context.Background(), toolchain.Gopls())
		return buildDoneMsg{err}
	}
}

// View renders the phase: block screen, confirm list, or the inner
// bootstrap install view.
func (m *Model) View() string {
	switch m.phase {
	case phaseBlocked:
		return m.blockView()
	case phaseConfirm:
		return m.confirmView()
	case phaseBuilding:
		base := ""
		if m.inner != nil {
			base = m.inner.View() + "\n"
		}
		if m.buildErr != "" {
			return base + theme.DangerText().Render("gopls build failed: "+m.buildErr) +
				"\n" + theme.Hint().Render("LSP will refuse until built — press enter to continue")
		}
		return base + theme.Hint().Render("building gopls from source…")
	case phaseInstalling:
		if m.inner != nil {
			return m.inner.View()
		}
		return ""
	default:
		return ""
	}
}

func (m *Model) blockView() string {
	w := max(m.width-14, 30) // panel padding + centering margins
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
	p := kit.NewPanel("dhi "+m.version, false)
	p.SetContent(lines...)
	p.Width = max(m.width-6, 40)
	p.Height = max(m.height-4, 12)
	return kit.Center(p.View(), p.Width, p.Height)
}

func (m *Model) confirmView() string {
	w := max(m.width-14, 30)
	var lines []string
	push := func(s string) { lines = append(lines, wrapLine(s, w)...) }
	push("missing hermetic components:")
	lines = append(lines, "")
	for _, o := range m.decision.Offer {
		push("  · " + o)
	}
	for _, warn := range m.decision.Warnings {
		lines = append(lines, "")
		push(theme.WarningText().Render(warn))
	}
	lines = append(lines, "",
		theme.Hint().Render("i install now · enter skip (capabilities refuse until installed)"))
	p := kit.NewPanel("dhi "+m.version, false)
	p.SetContent(lines...)
	p.Width = max(m.width-6, 40)
	p.Height = max(m.height-4, 12)
	return kit.Center(p.View(), p.Width, p.Height)
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
		out = append(out, cur.String())
		cur.Reset()
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
