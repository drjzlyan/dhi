package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
)

func writeChatSkill(t *testing.T, h *chatHarness, slug, front string) {
	t.Helper()
	dir := filepath.Join(h.ws.Root, ".dhi", "skills", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: " + slug + "\ndescription: runs " + slug + "\n" + front + "---\nsteps\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChatSlashSkillCompletionAndRefusal(t *testing.T) {
	h := newChatEditor(t, "ok")
	writeChatSkill(t, h, "deploy", "user-invocable: true\n")
	writeChatSkill(t, h, "plan", "user-invocable: true\n")
	writeChatSkill(t, h, "quiet", "")
	h.openFocused()
	typeKeys(h.m, "/dep")
	h.m.HandleKey("tab")
	if v := h.m.chat.input.Value; v != "/deploy " {
		t.Fatalf("value = %q", v)
	}
	h.m.chat.input.SetValue("/pl")
	h.m.HandleKey("tab")
	if !strings.Contains(h.m.chat.note, "built-in /plan wins") {
		t.Fatalf("note = %q", h.m.chat.note)
	}
	for in, want := range map[string]string{
		"/nope":  "unknown command /nope",
		"/quiet": "does not declare user-invocable",
	} {
		h.m.chat.input.SetValue(in)
		h.m.HandleKey("enter")
		if !strings.Contains(h.m.chat.note, want) || h.m.chat.input.Value != in {
			t.Errorf("%s: note %q input %q", in, h.m.chat.note, h.m.chat.input.Value)
		}
	}
	for _, e := range h.m.chat.bus.All(h.m.chat.channelName()) {
		if strings.HasPrefix(e.Text, "/nope") || strings.HasPrefix(e.Text, "/quiet") {
			t.Fatalf("a refused slash must not be posted: %+v", e)
		}
	}
}

func TestChatExportWritesUnderDhiExports(t *testing.T) {
	h := newChatEditor(t, "ok")
	if _, err := h.m.chat.bus.Post(bus.Message{Channel: h.m.chat.channelName(), Author: bus.Human, Text: "keep this"}); err != nil {
		t.Fatal(err)
	}
	h.openFocused()
	h.m.chat.input.SetValue("/export")
	h.m.HandleKey("enter")
	if !strings.Contains(h.m.chat.note, filepath.Join(".dhi", "exports")) {
		t.Fatalf("note = %q", h.m.chat.note)
	}
	m, _ := filepath.Glob(filepath.Join(h.ws.Root, ".dhi", "exports", "*.md"))
	if len(m) != 1 {
		t.Fatalf("exports = %v", m)
	}
}
