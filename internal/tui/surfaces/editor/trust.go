package editor

import (
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Restricted mode (F-148). A folder the user has not trusted still edits,
// searches and runs git; what a repo can steer — language servers, format
// on save, tasks, tests, the debugger — waits for one yes, recorded in
// DHI's own state (never in the repo).

// Trust is the user's record of trusted folders; cmd injects internal/trust.
type Trust interface {
	Trusted(dir string) bool
	Trust(dir string) error
	Untrust(dir string) error
}

// WithTrust turns restricted mode on. Without it every folder is trusted
// (embedding hosts and tests that do not run repo code).
func WithTrust(t Trust) Option { return func(m *Model) { m.trust = t } }

// trustAsk is the open trust card.
type trustAsk struct {
	dir  string // the member root being asked about
	what string // the feature that asked, e.g. "start gopls"
}

// trustRoot is the folder trust is asked for: the member repo holding dir,
// or dir itself outside every member.
func (m *Model) trustRoot(dir string) string {
	if dir == "" {
		return m.installDir()
	}
	if root, _, ok := m.repoRel(dir); ok {
		return root
	}
	return dir
}

// restricted reports whether what must not run in dir yet. When it must
// not, the active buffer says so by name (ADR-0011) and, once per folder
// per session, the trust card opens.
func (m *Model) restricted(dir, what string) bool {
	if m.trust == nil {
		return false
	}
	root := m.trustRoot(dir)
	if m.trust.Trusted(root) {
		return false
	}
	m.note(trustLine(what))
	if !m.trustDeclined[root] && m.trustCard == nil {
		m.trustCard = &trustAsk{dir: root, what: what}
	}
	return true
}

// trustLine is the one-line refusal for features that report through a
// returned message instead of the buffer note.
func trustLine(what string) string {
	return "restricted: trust this folder to " + what + " (:trust)"
}

// handleTrustKey is the card's focus trap: y trusts, n/esc leaves the
// folder restricted for this session.
func (m *Model) handleTrustKey(key string) bool {
	a := *m.trustCard
	switch key {
	case "y":
		m.trustCard = nil
		if err := m.trust.Trust(a.dir); err != nil {
			m.note("trust: " + err.Error())
			return true
		}
		m.note("trusted " + a.dir + " — " + a.what + " again")
		if e := m.active(); e != nil && e.Path() != "" {
			m.lspOpenDoc(e.Path(), e.Buffer().Text()) // the server that was held back starts now
		}
	case "n", "esc", "q":
		m.trustCard = nil
		if m.trustDeclined == nil {
			m.trustDeclined = map[string]bool{}
		}
		m.trustDeclined[a.dir] = true
	}
	return true
}

func (m *Model) trustCardView(base string) string {
	a := m.trustCard
	dim, accent := theme.TextMuted(), theme.AccentText()
	lines := append(kit.WrapWords("To "+a.what+", DHI would run programs this folder's contents control (language servers, formatters, build scripts, tests).", 70),
		"", "  "+accent.Render(a.dir), "",
		dim.Render("Editing, search and git work either way."),
		dim.Render("Kept in DHI's own state, not the repo. :untrust undoes it."),
		"", dim.Render("y trust this folder · n stay restricted"))
	box := kit.Modal{Title: "trust this folder?", Lines: lines, Width: min(maxInt(m.width-8, 30), 76)}
	return kit.Overlay(strings.Split(base, "\n"), box.View(), m.width, m.height)
}

// activeDir is the folder of the active file, "" without one.
func (m *Model) activeDir() string {
	if e := m.active(); e != nil && e.Path() != "" {
		return filepath.Dir(e.Path())
	}
	return ""
}

// trustCommand handles :trust and :untrust for the folder of the active
// file (else the first member).
func (m *Model) trustCommand(cmd string) (string, bool) {
	c := strings.TrimSpace(cmd)
	if c != "trust" && c != "untrust" {
		return "", false
	}
	if m.trust == nil {
		return c + ": unavailable here", true
	}
	root := m.trustRoot(m.activeDir())
	if root == "" || root == "." {
		return c + ": open a workspace first", true
	}
	if c == "untrust" {
		if err := m.trust.Untrust(root); err != nil {
			return err.Error(), true
		}
		if m.trust.Trusted(root) {
			return "untrust: " + root + " is still trusted through a parent folder", true
		}
		delete(m.trustDeclined, root)
		if m.lspMgr != nil {
			m.lspMgr.ShutdownAll() // F-148: running servers were started under the trust just withdrawn
		}
		return root + " is restricted: language servers stopped; format on save, tasks, tests and the debugger are off", true
	}
	if err := m.trust.Trust(root); err != nil {
		return "trust: " + err.Error(), true
	}
	if e := m.active(); e != nil && e.Path() != "" {
		m.lspOpenDoc(e.Path(), e.Buffer().Text())
	}
	return "trusted " + root, true
}
