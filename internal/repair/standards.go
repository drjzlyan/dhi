package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/skills"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Migrations to the standard formats (ADR-0043). Both touch only DHI's
// own per-user files under .dhi/ and <config>/dhi, so they are safe and
// run at launch, named.

// officialServers replaces the community MCP servers the old integrations
// catalog installed with the vendors' own remote servers, exactly as the
// vendors' plugins configure them (atlassian/atlassian-mcp-server and
// slackapi/slack-mcp-plugin, .mcp.json). Agents keep their mcp__<name>__*
// grants; the user signs in on first use.
var officialServers = map[string]string{
	"atlassian": `{"type":"http","url":"https://mcp.atlassian.com/v2/mcp"}`,
	"slack":     `{"type":"http","url":"https://mcp.slack.com/mcp","oauth":{"clientId":"1601185624273.8899143856786","callbackPort":3118}}`,
}

// community reports a legacy card that ran a community package.
func community(s mcpserver.Server) bool {
	argv := s.Command + " " + strings.Join(s.Args, " ")
	return strings.Contains(argv, "mcp-atlassian") || strings.Contains(argv, "@zencoderai/slack-mcp-server")
}

// mcpCards moves .dhi/mcp/<slug>.toml cards into .dhi/mcp.json.
func mcpCards(in Input) (Repair, bool) {
	legacy, files, err := mcpserver.ReadLegacy(in.WS.Root)
	if err != nil || len(legacy) == 0 {
		return Repair{}, false
	}
	var swapped []string
	for _, s := range legacy {
		if _, ok := officialServers[s.Slug]; ok && community(s) {
			swapped = append(swapped, s.Slug)
		}
	}
	what := fmt.Sprintf("Move %d MCP server card(s) from .dhi/mcp/ into .dhi/mcp.json, the standard mcpServers format", len(legacy))
	if len(swapped) > 0 {
		what += "; " + strings.Join(swapped, " and ") + " now use the vendor's official server (sign in on first use)"
	}
	return Repair{
		ID: "mcp-standard-format", What: what, Safe: true,
		Apply: func() error {
			path := mcpserver.WorkspaceFile(in.WS.Root)
			for _, s := range legacy {
				entry := s.Entry()
				if official, ok := officialServers[s.Slug]; ok && community(s) {
					entry = json.RawMessage(official)
				}
				if err := mcpserver.Add(path, s.Slug, entry); err != nil {
					return err
				}
			}
			for _, f := range files {
				if err := os.Remove(f); err != nil {
					return err
				}
			}
			_ = os.Remove(filepath.Join(in.WS.Root, mcpserver.LegacyDir)) // only when now empty
			return nil
		},
	}, true
}

// legacySkill is the old single-file skill: frontmatter name,
// description and an optional script path, then the body.
type legacySkill struct {
	slug, desc, script, body string
}

var nonName = regexp.MustCompile(`[^a-z0-9]+`)

// specName turns an old slug into a spec name (lowercase, hyphens).
func specName(slug string) string {
	n := strings.Trim(nonName.ReplaceAllString(strings.ToLower(slug), "-"), "-")
	if len(n) > skills.MaxName {
		n = strings.TrimRight(n[:skills.MaxName], "-")
	}
	return n
}

func readLegacySkills(dir string) []legacySkill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []legacySkill
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		if !strings.HasPrefix(text, "---\n") {
			continue
		}
		front, body, ok := strings.Cut(text[4:], "\n---")
		if !ok {
			continue
		}
		k := legacySkill{slug: strings.TrimSuffix(e.Name(), ".md"), body: strings.TrimSpace(strings.TrimPrefix(body, "\n"))}
		for _, l := range strings.Split(front, "\n") {
			key, val, _ := strings.Cut(l, ":")
			switch strings.TrimSpace(key) {
			case "description":
				k.desc = strings.TrimSpace(val)
			case "script":
				k.script = strings.TrimSpace(val)
			}
		}
		if k.desc != "" && k.body != "" {
			out = append(out, k)
		}
	}
	return out
}

// skillFiles converts old <slug>.md skills (a workspace's .dhi/skills and
// yours) into <name>/SKILL.md directories; a declared script moves into
// the skill's scripts/ folder.
func skillFiles(in Input) (Repair, bool) {
	dirs := []string{filepath.Join(in.WS.Root, workspace.DirSkills)}
	if library.UserSkillsDir != "" {
		dirs = append(dirs, library.UserSkillsDir)
	}
	type job struct {
		dir string
		k   legacySkill
	}
	var jobs []job
	for _, d := range dirs {
		for _, k := range readLegacySkills(d) {
			jobs = append(jobs, job{d, k})
		}
	}
	if len(jobs) == 0 {
		return Repair{}, false
	}
	return Repair{
		ID:   "skills-standard-format",
		What: fmt.Sprintf("Convert %d skill file(s) to Agent Skills folders (<name>/SKILL.md), the format every coding agent reads", len(jobs)),
		Safe: true,
		Apply: func() error {
			renamed := map[string]string{} // old slug → spec name, when they differ
			for _, j := range jobs {
				name := specName(j.k.slug)
				if name == "" {
					continue
				}
				if name != j.k.slug {
					renamed[j.k.slug] = name
				}
				s := &skills.Skill{Name: name, Slug: name, Description: j.k.desc, Body: j.k.body}
				if j.k.script != "" {
					s.Body += "\n\nRun scripts/" + filepath.Base(j.k.script) + " with skill_run."
				}
				if len(s.Description) > skills.MaxDescription {
					s.Description = s.Description[:skills.MaxDescription]
				}
				if err := skills.Write(j.dir, s); err != nil {
					return fmt.Errorf("%s: %w", j.k.slug, err)
				}
				if j.k.script != "" {
					src := filepath.Join(j.dir, filepath.FromSlash(j.k.script))
					dst := filepath.Join(j.dir, name, "scripts", filepath.Base(j.k.script))
					if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
						return err
					}
					if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
						return err
					}
				}
				if err := os.Remove(filepath.Join(j.dir, j.k.slug+".md")); err != nil {
					return err
				}
			}
			return renameSkillRefs(in, renamed)
		},
	}, true
}

// renameSkillRefs points agents at the renamed skills, so none silently
// loses one (a dangling name is dropped at run time).
func renameSkillRefs(in Input, renamed map[string]string) error {
	if len(renamed) == 0 || in.Org == nil {
		return nil
	}
	roster, err := org.LoadRoster(in.WS)
	if err != nil {
		return err
	}
	for _, a := range roster {
		changed := false
		skills := append([]string(nil), a.Skills...)
		for i, k := range skills {
			if n, ok := renamed[k]; ok {
				skills[i], changed = n, true
			}
		}
		if !changed {
			continue
		}
		cp := *a
		cp.Skills = skills
		if err := in.Org.UpdateAgent(in.WS, &cp); err != nil {
			return err
		}
	}
	return nil
}
