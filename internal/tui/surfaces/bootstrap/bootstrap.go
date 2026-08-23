// Package bootstrap renders DHI's first-run toolchain installer: the
// brand hero above a live stage list driven by toolchain.Manager events.
// All animation is message-driven (explicit ticks), so rendering is fully
// deterministic under test.
package bootstrap

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// spinnerFrames renders the activity indicator; index advances per tick.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const tickInterval = 120 * time.Millisecond

type phase uint8

const (
	phaseRunning phase = iota
	phaseDone
	phaseFailed
)

type status uint8

const (
	statusPending status = iota
	statusActive
	statusOK
	statusFail
)

type row struct {
	label  string
	detail string
	status status
}

// Model is the bootstrap surface state.
type Model struct {
	version      string
	width        int
	height       int
	mgr          *toolchain.Manager
	manifestURL  string
	events       chan eventMsg
	rows         map[string]*row
	order        []string
	spinnerFrame int
	phase        phase
	errText      string
}

var _ surfaces.Surface = (*Model)(nil)

// New wires the surface to a manager and registry manifest URL.
func New(version string, mgr *toolchain.Manager, manifestURL string) *Model {
	m := &Model{
		version:     version,
		mgr:         mgr,
		manifestURL: manifestURL,
		events:      make(chan eventMsg, 64),
		rows:        map[string]*row{},
	}
	mgr.OnEvent = func(e toolchain.Event) {
		select {
		case m.events <- eventMsg(e):
		default: // never block the installer on a slow consumer
		}
	}
	return m
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "bootstrap", Title: "Bootstrap"} }

func (m *Model) Resize(w, h int) { m.width, m.height = w, h }

// Init starts the install, the event pump, and the animation clock.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.startInstall(), m.listen(), m.tick())
}

func (m *Model) HandleKey(string) bool { return false }

// Finished reports whether the gate may release the shell: success and
// failure both proceed — missing pieces degrade visibly (ADR-0005).
func (m *Model) Finished() bool { return m.phase != phaseRunning }

type eventMsg toolchain.Event

type installDoneMsg struct{ err error }

type tickMsg struct{}

func (m *Model) startInstall() tea.Cmd {
	url := m.manifestURL
	return func() tea.Msg {
		var err error
		if url == "" {
			err = m.mgr.InstallEmbedded(context.Background())
		} else {
			err = m.mgr.Install(context.Background(), url, nil)
		}
		return installDoneMsg{err}
	}
}

func (m *Model) listen() tea.Cmd {
	ch := m.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return ev
	}
}

func (m *Model) tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// Update folds pipeline events into renderable rows.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case eventMsg:
		m.applyEvent(toolchain.Event(msg))
		return m.listen()

	case installDoneMsg:
		if msg.err != nil {
			m.phase = phaseFailed
			m.errText = msg.err.Error()
			for _, r := range m.rows {
				if r.status == statusActive {
					r.status = statusFail
				}
			}
		} else {
			m.phase = phaseDone
			for _, r := range m.rows {
				if r.status == statusActive {
					r.status = statusOK
				}
			}
		}
		return nil

	case tickMsg:
		if m.phase != phaseRunning {
			return nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m.tick()
	}
	return nil
}

func (m *Model) applyEvent(ev toolchain.Event) {
	switch ev.Kind {
	case toolchain.EventManifestFetched:
		m.setRow("registry", "registry manifest", ev.Detail, statusOK)
	case toolchain.EventResolved:
		detail := ev.Detail
		if strings.Contains(detail, "0 action") {
			detail = "up to date"
		}
		m.setRow("plan", "plan", detail, statusOK)
	case toolchain.EventDownloadStart:
		m.setRow("tool/"+ev.Tool, ev.Tool+" download", "", statusActive)
	case toolchain.EventDownloadDone:
		m.setRow("tool/"+ev.Tool, ev.Tool+" verify", "sha256 ok", statusActive)
	case toolchain.EventVerified:
		m.setRow("tool/"+ev.Tool, ev.Tool+" extract", "sha256 ok", statusActive)
	case toolchain.EventExtracted:
		m.setRow("tool/"+ev.Tool, ev.Tool+" activate", "extracted", statusActive)
	case toolchain.EventActivated:
		m.setRow("tool/"+ev.Tool, ev.Tool, "activated", statusActive)
	case toolchain.EventToolDone:
		m.setRow("tool/"+ev.Tool, ev.Tool, "installed", statusOK)
	case toolchain.EventDone:
		if r, ok := m.rows["plan"]; ok && strings.Contains(ev.Detail, "up to date") {
			r.detail = "up to date"
		}
	}
}

func (m *Model) setRow(key, label, detail string, st status) {
	r, ok := m.rows[key]
	if !ok {
		r = &row{}
		m.rows[key] = r
		m.order = append(m.order, key)
	}
	if label != "" {
		r.label = label
	}
	if detail != "" {
		r.detail = detail
	}
	r.status = maxStatus(r.status, st)
}

// maxStatus keeps monotonic progression (pending < active < ok < fail);
// later events never erase a stronger state.
func maxStatus(a, b status) status {
	if b > a {
		return b
	}
	return a
}

// View renders hero + stage list, centered as one composition.
func (m *Model) View() string {
	hero := strings.Split(branding.HeroBlock(m.version), "\n")

	headline := "preparing hermetic toolchain"
	switch m.phase {
	case phaseDone:
		headline = theme.SuccessText().Render(theme.GlyphCheck + " toolchain ready")
	case phaseFailed:
		headline = theme.DangerText().Render(theme.GlyphCross + " bootstrap failed")
	}
	body := append([]string{}, hero...)
	body = append(body, "", headline, "")

	for _, key := range m.order {
		r := m.rows[key]
		body = append(body, stageLine(r, m.spinnerFrame))
	}
	if m.phase == phaseFailed {
		detail := m.errText
		if len(detail) > 96 {
			detail = detail[:93] + "…"
		}
		body = append(body, theme.DangerText().Render("  "+detail))
	}

	return kit.Center(strings.Join(body, "\n"), m.width, m.height)
}

func stageLine(r *row, frame int) string {
	var glyph, label, detail string
	label = theme.TabActive().Render(r.label)
	detail = theme.TextDim().Render(r.detail)
	switch r.status {
	case statusOK:
		glyph = theme.SuccessText().Render(theme.GlyphCheck)
	case statusFail:
		glyph = theme.DangerText().Render(theme.GlyphCross)
	case statusActive:
		glyph = theme.Brand().Render(spinnerFrames[frame%len(spinnerFrames)])
	default:
		glyph = theme.TextDim().Render("·")
	}
	line := "  " + glyph + "  " + label
	if r.detail != "" {
		line += "  " + detail
	}
	return line
}
