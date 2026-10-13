package pack

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
)

// MaxScriptBytes caps one skill script a pack may ship (F-091): it is
// shown in full to the human before install.
const MaxScriptBytes = 64 << 10

// SkillPayload is one skill a pack ships, validated: the parsed doc and,
// when it declares one, the script's bytes (F-091).
type SkillPayload struct {
	Slug   string
	Skill  *library.Skill
	Script []byte // nil = instruction-only
}

// ReadSkills validates every skill in the pack at dir without writing
// anything: the doc parses (size-capped), and a declared script exists
// inside the pack beside its doc, under the size cap. This is what a
// reviewer sees before an install (F-091).
func ReadSkills(dir string) ([]SkillPayload, *Spec, error) {
	spec, err := ReadSpec(dir)
	if err != nil {
		return nil, nil, err
	}
	var out []SkillPayload
	for _, rel := range spec.Skills {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, nil, fmt.Errorf("pack: skill path %q escapes the pack", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		slug := strings.TrimSuffix(filepath.Base(rel), ".md")
		k, err := library.ParseSkill(slug, data)
		if err != nil {
			return nil, nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		p := SkillPayload{Slug: slug, Skill: k}
		if script := strings.TrimSpace(k.Script); script != "" {
			src := path.Join(path.Dir(rel), path.Clean(script))
			if strings.HasPrefix(src, "../") || src == ".." {
				return nil, nil, fmt.Errorf("pack: %s: script %q escapes the pack", rel, script)
			}
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src)))
			if err != nil {
				return nil, nil, fmt.Errorf("pack: %s: script %s: %w", rel, script, err)
			}
			if len(b) > MaxScriptBytes {
				return nil, nil, fmt.Errorf("pack: %s: script %s is over %d KiB", rel, script, MaxScriptBytes>>10)
			}
			p.Script = b
		}
		out = append(out, p)
	}
	return out, spec, nil
}

// WriteSkills installs payloads into a skills dir (a workspace's
// .dhi/skills or library.UserSkillsDir), scripts executable beside them.
func WriteSkills(dest string, payloads []SkillPayload) error {
	for _, p := range payloads {
		if p.Script != nil {
			dst := filepath.Join(dest, filepath.FromSlash(path.Clean(p.Skill.Script)))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, p.Script, 0o755); err != nil {
				return fmt.Errorf("pack: write script for %s: %w", p.Slug, err)
			}
		}
		if err := library.WriteSkillAt(dest, p.Skill); err != nil {
			return fmt.Errorf("pack: write skill %s: %w", p.Slug, err)
		}
	}
	return nil
}

// skillScripts lists the script paths payloads install, for provenance.
func skillScripts(payloads []SkillPayload) []string {
	var out []string
	for _, p := range payloads {
		if p.Script != nil {
			out = append(out, path.Clean(p.Skill.Script))
		}
	}
	return out
}
