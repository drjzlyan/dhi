package ideator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Artifact export (F-059): an approved idea leaves the canvas as a file
// in a member repo, a card on the board, or an issue in the team's
// tracker. The tracker route hands the artifact to an agent that holds
// the tracker's MCP tools — DHI never calls a tracker itself — so the
// filing shows up in the session channel like any other agent work.

// exportDestinations are the toggle values of the export form.
var exportDestinations = []string{"repo file", "task card", "tracker (via agent)"}

// exportCap bounds the artifact text copied into a task comment or an
// agent request; the full file stays on disk and is referenced by path.
const exportCap = 4000

// openExport starts the export form for the artifact under the cursor.
func (m *Model) openExport(rel string) {
	sess, ok := m.openSession()
	if !ok {
		return
	}
	m.form = formState{kind: fExport, orig: rel, fields: []field{
		toggleField("to     ", exportDestinations),
		textField("target ", m.exportDefault(sess, rel, 0)),
	}}
}

// exportDefault proposes a target for destination d: a docs path in the
// first member, a slug from the artifact name, or the moderator.
func (m *Model) exportDefault(sess ideation.Session, rel string, d int) string {
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	switch d {
	case 1:
		return ideation.Slugify(base)
	case 2:
		if sess.Moderator != "" {
			return sess.Moderator
		}
		if len(sess.Agents) > 0 {
			return sess.Agents[0]
		}
		return ""
	}
	member := ""
	if m.ws != nil {
		if ms := m.ws.Members(); len(ms) > 0 {
			member = ms[0].Name
		}
	}
	return member + "/docs/" + filepath.Base(rel)
}

// exportCycled refreshes the target when the destination toggle moves.
func (m *Model) exportCycled() {
	f := &m.form
	if sess, ok := m.openSession(); ok {
		f.fields[1].runes = []rune(m.exportDefault(sess, f.orig, f.fields[0].val))
	}
	f.err, f.armed = "", false
}

// submitExport performs the chosen export.
func (m *Model) submitExport() {
	f := &m.form
	sess, ok := m.openSession()
	if !ok || m.store == nil {
		return
	}
	rel, target := f.orig, strings.TrimSpace(f.fields[1].text())
	if target == "" {
		f.err = "target required"
		return
	}
	data, err := os.ReadFile(m.store.ArtifactPath(sess.ID, rel))
	if err != nil {
		f.err = "read artifact: " + err.Error()
		return
	}
	var flash string
	switch f.fields[0].val {
	case 0:
		flash, err = m.exportToFile(target, data, f.armed)
		if errors.Is(err, errExists) {
			f.err, f.armed = target+" exists — enter again to overwrite", true
			return
		}
	case 1:
		flash, err = m.exportToTask(sess, rel, target, data)
	case 2:
		flash, err = m.exportToTracker(sess, rel, target, data)
	}
	if err != nil {
		f.err = err.Error()
		return
	}
	m.closeFormWithFlash(flash)
}

var errExists = errors.New("exists")

// exportToFile writes the artifact to a workspace vpath ("api/docs/x.md").
func (m *Model) exportToFile(target string, data []byte, overwrite bool) (string, error) {
	if m.ws == nil {
		return "", errors.New("no workspace")
	}
	vp, err := workspace.ParseVPath(target)
	if err != nil {
		return "", fmt.Errorf("target must be member/path (e.g. api/docs/plan.md): %w", err)
	}
	abs, err := m.ws.Resolve(vp)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil && !overwrite {
		return "", errExists
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	if m.openInEditor != nil {
		m.openInEditor([]string{abs})
	}
	return "exported to " + target, nil
}

// exportToTask creates a board card titled from the artifact, its text
// as the first comment.
func (m *Model) exportToTask(sess ideation.Session, rel, slug string, data []byte) (string, error) {
	if m.tasks == nil {
		return "", errors.New("task board unavailable")
	}
	if err := m.tasks.Create(slug, exportTitle(rel, data), "", ""); err != nil {
		return "", err
	}
	body := "From ideation `" + ideation.VPathFor(sess, rel) + "`:\n\n" + capText(string(data))
	if err := m.tasks.AddComment(slug, busHuman, body); err != nil {
		return "", err
	}
	return "task " + slug + " created from " + rel, nil
}

// exportToTracker asks an agent with tracker tools to file the artifact.
func (m *Model) exportToTracker(sess ideation.Session, rel, agent string, data []byte) (string, error) {
	if m.bus == nil {
		return "", errors.New("no message bus — agents are not running")
	}
	agent = strings.TrimPrefix(agent, "@")
	text := "@" + agent + " please file `" + ideation.VPathFor(sess, rel) +
		"` as an issue in our tracker with your MCP tools (title: " + exportTitle(rel, data) +
		"), then reply with the link.\n\n" + capText(string(data))
	posted, err := m.bus.Post(bus.Message{Channel: sess.Channel, Author: busHuman, Text: text})
	if err != nil {
		return "", err
	}
	m.requestTurn(posted)
	return "asked @" + agent + " to file " + rel + " — watch " + sess.Channel, nil
}

// exportTitle is the artifact's first Markdown heading, else its name.
func exportTitle(rel string, data []byte) string {
	for _, l := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(strings.TrimLeft(l, "#")); strings.HasPrefix(l, "#") && t != "" {
			return t
		}
	}
	return strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
}

func capText(s string) string {
	if len(s) <= exportCap {
		return s
	}
	return s[:exportCap] + "\n\n… (truncated; the full text is in the artifact file)"
}
