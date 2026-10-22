package skills

import (
	"html"
	"strings"
)

// Progressive disclosure, as the agentskills.io client guide lays out:
// agents see a short catalog (name, description, location) for the
// skills they are assigned, and load a skill's instructions only when a
// task matches — by reading SKILL.md, or through the skill_activate tool
// on runtimes that cannot read files.

// catalogIntro is the guide's behavioural instruction for file-read
// activation, with the dedicated tool as the alternative.
const catalogIntro = `Your skills are part of your instructions: the team chose them for the work you do.
Before you start a task, check the list below. When the task matches a skill's description, load that skill first — read the SKILL.md at its location with your file-read tool (or call skill_activate with its name) — and follow it; say which skill you used in your summary.
When a skill references relative paths, resolve them against the skill's directory (the parent of SKILL.md) and use absolute paths in tool calls. Run a skill's scripts with skill_run.`

// Catalog renders the <available_skills> section; "" when there are none
// (no empty catalog, as the guide asks).
func Catalog(list []Skill) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(catalogIntro)
	b.WriteString("\n\n<available_skills>\n")
	for _, s := range list {
		b.WriteString("  <skill>\n")
		b.WriteString("    <name>" + html.EscapeString(s.Slug) + "</name>\n")
		b.WriteString("    <description>" + html.EscapeString(s.Description) + "</description>\n")
		if loc := s.Location(); loc != "" {
			b.WriteString("    <location>" + html.EscapeString(loc) + "</location>\n")
		}
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// Activation is what skill_activate returns: the instructions wrapped so
// the agent (and compaction) can tell them apart, the skill directory,
// and its bundled files, listed but not read.
func Activation(s Skill) string {
	var b strings.Builder
	b.WriteString(`<skill_content name="` + html.EscapeString(s.Slug) + `">` + "\n")
	b.WriteString(strings.TrimSpace(s.Body) + "\n")
	if s.Dir != "" {
		b.WriteString("\nSkill directory: " + s.Dir + "\n")
		b.WriteString("Relative paths in this skill are relative to the skill directory.\n")
		if files, more := Resources(s.Dir, 50); len(files) > 0 {
			b.WriteString("\n<skill_resources>\n")
			for _, f := range files {
				b.WriteString("  <file>" + html.EscapeString(f) + "</file>\n")
			}
			if more {
				b.WriteString("  <!-- more files not listed -->\n")
			}
			b.WriteString("</skill_resources>\n")
		}
	}
	b.WriteString("</skill_content>")
	return b.String()
}
