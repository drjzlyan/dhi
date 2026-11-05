package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/skills"
)

func slashSkill(slug string, declared bool) skills.Skill {
	t := true
	k := skills.Skill{Slug: slug, Name: slug, Description: "d " + slug}
	if declared {
		k.UserInvocable = &t
	}
	return k
}

type skillRT struct {
	stubTurner
	list    []skills.Skill
	exports []string
}

func (s *skillRT) UserSkills() []skills.Skill { return s.list }
func (s *skillRT) ExportConversation(ch string, th int64, f string) (string, error) {
	s.exports = append(s.exports, ch+"|"+f)
	return "/ws/.dhi/exports/x." + f, nil
}

func TestBuiltinSetCoversEveryComposerParser(t *testing.T) {
	for _, in := range []string{"/model", "/fork", "/new", "/compact", "/plan x", "/review x", "/ask x", "/export", "/yes", "/no"} {
		name, _, ok := skills.Head(in)
		if !ok || !skills.Builtin(name) {
			t.Errorf("%q: head %q is not reserved", in, name)
		}
	}
	for _, in := range []string{"/fork", "/new", "/compact", "/export"} {
		if cmd, _, ok := parseConvCmd(in); !ok || !skills.Builtin(cmd) {
			t.Errorf("parseConvCmd(%q) = %q,%v", in, cmd, ok)
		}
	}
	for _, in := range []string{"/plan x", "/review x", "/ask x"} {
		if mode, _, ok := parseModeCmd(in); !ok || !skills.Builtin(mode) {
			t.Errorf("parseModeCmd(%q) = %q,%v", in, mode, ok)
		}
	}
	if _, ok := parseModelCmd("/model"); !ok || !skills.Builtin("model") {
		t.Error("/model")
	}
}

func TestSlashTabCompletesSkillsAndBuiltinsWin(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.rt = &skillRT{list: []skills.Skill{slashSkill("deploy", true), slashSkill("plan", true), slashSkill("notes", false)}}
	p.focus = true
	p.input.SetValue("/dep")
	p.composerKey("tab")
	if p.input.Value != "/deploy " {
		t.Fatalf("value = %q", p.input.Value)
	}
	p.input.SetValue("/p")
	p.composerKey("tab")
	if p.input.Value != "/plan " || !strings.Contains(p.note, "built-in /plan wins") {
		t.Fatalf("value %q note %q (only the built-in /plan is offered, and it says so)", p.input.Value, p.note)
	}
	p.input.SetValue("/no")
	p.composerKey("tab")
	if strings.Contains(p.note, "notes") {
		t.Errorf("an undeclared skill must not be offered: %q", p.note)
	}
}

func TestSlashSkillPostsAndUnknownIsRefusedByName(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	rt := &skillRT{list: []skills.Skill{slashSkill("deploy", true), slashSkill("notes", false)}}
	p.rt = rt
	p.channels = []string{"#general", "dm:alice"}
	p.active, p.focus = 1, true

	p.input.SetValue("/deploy staging")
	p.composerKey("enter")
	if len(rt.handled) != 1 || rt.handled[0].Text != "/deploy staging" {
		t.Fatalf("a skill command posts as typed and the runtime activates it: %+v", rt.handled)
	}
	p.focus = true
	p.input.SetValue("/nope x")
	p.composerKey("enter")
	if !strings.Contains(p.flash, "unknown command /nope") || p.input.Value != "/nope x" || len(rt.handled) != 1 {
		t.Fatalf("flash %q input %q handled %d", p.flash, p.input.Value, len(rt.handled))
	}
	p.input.SetValue("/notes x")
	p.composerKey("enter")
	if !strings.Contains(p.flash, "does not declare user-invocable") {
		t.Fatalf("flash = %q", p.flash)
	}
}

func TestExportCommand(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	rt := &skillRT{}
	p.rt = rt
	p.channels = []string{"#general", "dm:alice"}
	p.active = 1
	if !p.conversationCommand("/export") || !strings.Contains(p.flash, "open the conversation") {
		t.Fatalf("no thread: %q", p.flash)
	}
	root, _ := p.bus.Post(bus.Message{Channel: "dm:alice", Author: bus.Human, Text: "hi"})
	p.threadID = root.ID
	p.conversationCommand("/export json")
	if len(rt.exports) != 1 || rt.exports[0] != "dm:alice|json" || !strings.Contains(p.note, ".dhi/exports/x.json") {
		t.Fatalf("exports %v note %q", rt.exports, p.note)
	}
	p.active = 0
	p.conversationCommand("/export")
	if !strings.Contains(p.flash, "direct message") {
		t.Fatalf("channel: %q", p.flash)
	}
}
