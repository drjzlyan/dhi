package editor

import (
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/skills"
)

// G-X3 in the editor chat: user-invocable skills as /<name>, and /export.
// The parsing and the built-in set are shared with the workspace composer
// (internal/agentkit/skills); the runtime activates the skill on the turn.

func (c *chatModel) skillList() []skills.Skill {
	if c.rt == nil {
		return nil
	}
	return c.rt.UserSkills()
}

// slashComplete completes the leading "/word" on tab (true = handled).
func (c *chatModel) slashComplete() bool {
	v := c.input.Value
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \n") {
		return false
	}
	list := c.skillList()
	cands := skills.Complete(v[1:], list)
	switch len(cands) {
	case 0:
		c.note = "no command starts with " + v
	case 1:
		c.input.SetValue("/" + cands[0] + " ")
		c.note = ""
	default:
		c.input.SetValue("/" + skills.CommonPrefix(cands))
		c.note = "/" + strings.Join(cands, "  /")
	}
	if n := skills.ShadowNote(list); n != "" {
		c.note = strings.TrimSpace(c.note + "  · " + n)
	}
	return true
}

// slashCommand handles /export and refuses a "/word" that is neither a
// built-in nor a user-invocable skill, by name (ADR-0011), rather than
// sending it to the agent as prose. true = handled; the text stays.
func (c *chatModel) slashCommand(text string) bool {
	name, args, ok := skills.Head(text)
	if !ok || !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false
	}
	if name == "export" {
		c.export(args)
		return true
	}
	if skills.Builtin(name) {
		return false
	}
	list := c.skillList()
	if _, _, found := skills.Invocation(text, list); found {
		return false
	}
	for _, k := range list {
		if k.Slug == name {
			c.note = "/" + name + " is a skill that does not declare user-invocable: true in its SKILL.md"
			return true
		}
	}
	c.note = "unknown command /" + name + " (tab lists commands)"
	return true
}

// export writes this conversation to .dhi/exports/ and says where.
func (c *chatModel) export(format string) {
	if c.rt == nil {
		c.note = "/export needs the crew running"
		return
	}
	path, err := c.rt.ExportConversation(c.channelName(), 0, format)
	if err != nil {
		c.note = "export failed: " + err.Error()
		return
	}
	c.note = "exported to " + filepath.ToSlash(path) + " (secrets redacted)"
}

// fillSkill puts "/<name> " in the composer and focuses it (palette).
func (c *chatModel) fillSkill(name string) {
	c.Focus()
	c.input.SetValue("/" + name + " ")
}
