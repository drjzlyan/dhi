package workspace

import (
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/skills"
)

// G-X3: user-invocable skills as slash commands, and /export, in the
// composer. The parsing, the built-in set and the completion are shared
// with the editor chat (internal/agentkit/skills); the runtime activates the
// skill when the turn runs, so a skill message posts like any other.

// skillSource is the runtime's skill list; *runtime.Runtime satisfies it.
type skillSource interface{ UserSkills() []skills.Skill }

// exporter is the runtime's /export.
type exporter interface {
	ExportConversation(channel string, thread int64, format string) (string, error)
}

func (p *chatPane) skillList() []skills.Skill {
	if s, ok := p.rt.(skillSource); ok {
		return s.UserSkills()
	}
	return nil
}

// slashComplete completes the "/word" being typed on tab (true = handled).
func (p *chatPane) slashComplete() bool {
	v := p.input.Value
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \n") {
		return false
	}
	list := p.skillList()
	p.flash, p.note = "", ""
	c := skills.Complete(v[1:], list)
	switch len(c) {
	case 0:
		p.note = "no command starts with " + v
	case 1:
		p.input.SetValue("/" + c[0] + " ")
	default:
		p.input.SetValue("/" + skills.CommonPrefix(c))
		p.note = "/" + strings.Join(c, "  /")
	}
	if n := skills.ShadowNote(list); n != "" {
		p.note = strings.TrimSpace(p.note + "  · " + n)
	}
	return true
}

// slashRefuse stops a "/word" that is no built-in and no user-invocable
// skill from going to the agent as prose, naming the reason (ADR-0011).
// The text stays in the composer.
func (p *chatPane) slashRefuse(text string) bool {
	name, _, ok := skills.Head(text)
	if !ok || skills.Builtin(name) {
		return false
	}
	list := p.skillList()
	if _, _, found := skills.Invocation(text, list); found {
		return false
	}
	p.note = ""
	for _, k := range list {
		if k.Slug == name {
			p.flash = "/" + name + " is a skill that does not declare user-invocable: true in its SKILL.md"
			return true
		}
	}
	p.flash = "unknown command /" + name + " (tab lists commands; to send it as text, put a word before the slash)"
	return true
}

// export writes the open conversation to .dhi/exports/ (reply = the path).
func (p *chatPane) export(ch, format string) {
	x, ok := p.rt.(exporter)
	if !ok {
		p.flash = "/export needs the crew running"
		return
	}
	if p.threadID == 0 {
		p.flash = "open the conversation to export first (t on a message), then /export"
		return
	}
	path, err := x.ExportConversation(ch, p.threadID, format)
	if err != nil {
		p.flash = "export failed: " + err.Error()
		return
	}
	p.note = "exported to " + filepath.ToSlash(path) + " (secrets redacted)"
}
