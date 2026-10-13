package settings

import (
	"context"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- PROJECT section (F-080): the brief every employee reads ----

func (m *Model) loadBrief() (brief.Brief, bool, error) {
	if m.d.WS == nil {
		return brief.Brief{}, false, nil
	}
	return brief.Load(m.d.WS.Root)
}

func (m *Model) projectKey(key string) bool {
	if m.d.WS == nil {
		return false
	}
	switch key {
	case "e", "enter":
		b, _, err := m.loadBrief()
		if err != nil {
			m.flash = "failed: " + err.Error()
			return true
		}
		m.openBriefForm("edit project brief", b)
		return true
	case "d":
		// Redraft from the repo, keeping whatever is already written.
		b, _, _ := m.loadBrief()
		draft := brief.Detect(m.d.WS.Root, m.memberDirs())
		for _, f := range brief.Fields {
			if strings.TrimSpace(b.Get(f.Key)) == "" {
				_ = b.Set(f.Key, draft.Get(f.Key))
			}
		}
		m.openBriefForm("project brief — drafted from the repo", b)
		return true
	case "i":
		m.flash = m.askInterview()
		return true
	}
	return false
}

func (m *Model) memberDirs() []string {
	var dirs []string
	for _, mb := range m.d.WS.Members() {
		dirs = append(dirs, mb.Path)
	}
	return dirs
}

func (m *Model) openBriefForm(title string, b brief.Brief) {
	fields := make([]kit.Field, len(brief.Fields))
	for i, f := range brief.Fields {
		fields[i] = kit.NewTextField(f.Label, strings.ReplaceAll(b.Get(f.Key), "\n", `\n`))
	}
	m.openFormDialog(title, dlgProject, "", fields...)
}

func (m *Model) submitBrief() {
	var b brief.Brief
	for i, f := range brief.Fields {
		_ = b.Set(f.Key, strings.ReplaceAll(strings.TrimSpace(m.dform.Values()[i]), `\n`, "\n"))
	}
	if err := brief.Save(m.d.WS.Root, b); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.flash = "project brief saved — employees read it from their next turn"
}

// askInterview posts the human's request to the first employee who
// leads a team, in that team's channel.
func (m *Model) askInterview() string {
	if m.d.Bus == nil || m.d.Org == nil {
		return "failed: chat is unavailable here"
	}
	for _, t := range m.d.Org.Teams() {
		if t.Lead == "" || t.Lead == org.Human {
			continue
		}
		posted, err := m.d.Bus.Post(bus.Message{Channel: "#" + t.Name, Author: bus.Human,
			Text: "@" + t.Lead + " " + brief.InterviewRequest})
		if err != nil {
			return "failed: " + err.Error()
		}
		if m.d.Runtime == nil {
			return "asked " + t.Lead + " in #" + t.Name + " — they answer once employees are running"
		}
		// A post alone wakes nobody: hand it to the runtime, as chat does.
		m.d.Runtime.Handle(context.Background(), posted)
		return "asked " + t.Lead + " to interview you — answer in #" + t.Name
	}
	return "no team has an employee as lead — add one in TEAMS (a), then press i again"
}

func (m *Model) projectView(w int) []string {
	if m.d.WS == nil {
		return []string{theme.TextDim().Render("(project brief unavailable — not inside a workspace)")}
	}
	b, ok, err := m.loadBrief()
	var out []string
	for _, l := range kit.WrapWords("What the project is and how you work. Every employee reads this on every task.", max(w, 20)) {
		out = append(out, theme.TextDim().Render(l))
	}
	out = append(out, "")
	switch {
	case err != nil:
		return append(out, theme.DangerText().Render(err.Error()), "",
			theme.Hint().Render("e rewrites it from a form"))
	case !ok || b.Empty():
		return append(out, theme.TextDim().Render("No brief yet. d drafts one from the repo, e writes it, i asks your lead to interview you."))
	}
	for _, f := range brief.Fields {
		v := strings.TrimSpace(b.Get(f.Key))
		label := theme.SectionHeader().Render(padTo(f.Title, 14))
		if v == "" {
			out = append(out, label+theme.TextDim().Render("—"))
			continue
		}
		for i, l := range kit.WrapWords(strings.ReplaceAll(v, "\n", " · "), max(w-14, 20)) {
			if i > 0 {
				label = padTo("", 14)
			}
			out = append(out, label+theme.TextStyle().Render(l))
		}
	}
	return out
}
