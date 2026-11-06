package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
)

func writeSkillCmd(t *testing.T, h *harness, slug, front string) {
	t.Helper()
	dir := filepath.Join(h.ws.Root, ".dhi", "skills", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: " + slug + "\ndescription: runs " + slug + "\n" + front + "---\nDEPLOY-STEPS for " + slug + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUserInvocableSkillRunsActivated(t *testing.T) {
	h := episodeHarness(t)
	writeSkillCmd(t, h, "deploy", "user-invocable: true\n")
	found := false
	for _, k := range h.rt.UserSkills() {
		found = found || (k.Slug == "deploy" && k.Slash())
	}
	if !found {
		t.Fatal("UserSkills must list the declared skill")
	}
	trig := dmTrigger(t, h, "/deploy staging")
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	args := spawnArgs(t, h, 1)
	for _, want := range []string{`<skill_content name="deploy">`, "DEPLOY-STEPS for deploy", "with: staging"} {
		if !strings.Contains(args, want) {
			t.Errorf("argv lacks %q:\n%s", want, args)
		}
	}
}

func TestSkillWithoutDeclarationOrFromAnAgentIsNotActivated(t *testing.T) {
	h := episodeHarness(t)
	writeSkillCmd(t, h, "notes", "") // not declared
	writeSkillCmd(t, h, "ship", "user-invocable: true\n")
	if err := h.rt.Turn(context.Background(), "scout", dmTrigger(t, h, "/notes x")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spawnArgs(t, h, 1), "skill_content") {
		t.Fatal("an undeclared skill must not be activated by a slash")
	}
	// An agent's message never activates a skill.
	agentMsg := mustPost(t, h, bus.Message{Channel: "dm:scout", Author: "other", Text: "/ship now"})
	if err := h.rt.Turn(context.Background(), "scout", agentMsg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spawnArgs(t, h, 2), "skill_content") {
		t.Fatal("only the human's slash activates a skill")
	}
}

func TestExportWritesRedactedFencedMarkdownAndJSON(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Redact = func(s string) string { return strings.ReplaceAll(s, "TOPSECRET", "‹secret:k›") }
	root := mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "remember TOPSECRET please"})
	if err := h.rt.Turn(context.Background(), "scout", root); err != nil {
		t.Fatal(err)
	}
	follow := mustPost(t, h, bus.Message{Channel: "dm:scout", Thread: root.ID, Author: bus.Human, Text: "and again"})
	if err := h.rt.Turn(context.Background(), "scout", follow); err != nil {
		t.Fatal(err)
	}
	path, err := h.rt.ExportConversation("dm:scout", root.ID, "md")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(h.ws.Root, ".dhi", "exports"); filepath.Dir(path) != want {
		t.Fatalf("written to %s, want under %s", path, want)
	}
	b, _ := os.ReadFile(path)
	md := string(b)
	for _, want := range []string{"# Conversation with scout", "- Agent: scout", "- CLI: claude", "- Model:", "agent default", "- Tokens: 4 in, 2 out", "‹secret:k›", "## scout", `<untrusted_data source="message-from-scout">`, "reply 2"} {
		if !strings.Contains(md, want) {
			t.Errorf("export lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "TOPSECRET") {
		t.Fatalf("secret leaked:\n%s", md)
	}
	jp, err := h.rt.ExportConversation("dm:scout", root.ID, "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Header   map[string]any
		Messages []struct {
			Author, Text string
			Fenced       bool
		}
	}
	jb, _ := os.ReadFile(jp)
	if err := json.Unmarshal(jb, &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, jb)
	}
	if doc.Header["agent"] != "scout" || len(doc.Messages) < 2 {
		t.Fatalf("doc = %+v", doc)
	}
	last := doc.Messages[len(doc.Messages)-1]
	if last.Author != "scout" || !last.Fenced || !strings.Contains(last.Text, "<untrusted_data") {
		t.Fatalf("agent text must stay fenced: %+v", last)
	}
	if _, err := h.rt.ExportConversation("dm:scout", root.ID, "pdf"); err == nil {
		t.Fatal("unknown format must be refused")
	}
	if _, err := h.rt.ExportConversation("dm:scout", 9999, "md"); err == nil {
		t.Fatal("an empty conversation must be refused")
	}
}

func TestExportUsesConversationModelOverride(t *testing.T) {
	h := episodeHarness(t)
	root := mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "hello"})
	if err := h.rt.Turn(context.Background(), "scout", root); err != nil {
		t.Fatal(err)
	}
	key := "thread:dm:scout:" + strconv.FormatInt(root.ID, 10)
	if err := h.rt.cfg.Episodes.Update("scout", key, func(e *episode.Episode) { e.Model = "opus-x" }); err != nil {
		t.Fatal(err)
	}
	path, err := h.rt.ExportConversation("dm:scout", root.ID, "md")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "opus-x (conversation override (/model))") {
		t.Fatalf("model source missing:\n%s", b)
	}
}

func TestImageMentionParsing(t *testing.T) {
	got := ImageMentions(`look @image:a/b.png and @image:"my dir/c d.jpg", then @image:x.webp.`)
	want := []string{"a/b.png", "my dir/c d.jpg", "x.webp"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImageAttachesForClaudeByPath(t *testing.T) {
	h := episodeHarness(t)
	writePNG(t, filepath.Join(h.ws.Root, "api", "shot.png"))
	trig := dmTrigger(t, h, "what is in @image:api/shot.png ?")
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	args := spawnArgs(t, h, 1)
	if !strings.Contains(args, "shot.png") || !strings.Contains(args, "attached image files") {
		t.Fatalf("the image path must reach the prompt:\n%s", args)
	}
}

func TestImageRefusedByNameWhereTheCLIHasNone(t *testing.T) {
	h := episodeHarness(t)
	writePNG(t, filepath.Join(h.ws.Root, "api", "shot.png"))
	cur, _ := h.rt.cfg.CLIs.Get("cursor-agent")
	h.rt.agents["scout"].cli = cur
	old := cliRetryBackoff
	cliRetryBackoff = time.Millisecond
	defer func() { cliRetryBackoff = old }()
	_ = h.rt.Turn(context.Background(), "scout", dmTrigger(t, h, "see @image:api/shot.png"))
	if hasPrefix(h.bus.History("dm:scout", 0), "cursor-agent cannot take an image") == nil {
		t.Fatalf("refusal must name the CLI: %v", h.bus.History("dm:scout", 0))
	}
	if _, err := os.Stat(filepath.Join(h.dumpDir, "spawn-1")); err == nil {
		t.Fatal("nothing may be started")
	}
}

func TestImagePathRules(t *testing.T) {
	h := episodeHarness(t)
	root := h.ws.Root
	outside := filepath.Join(t.TempDir(), "o.png")
	writePNG(t, outside)
	if err := os.WriteFile(filepath.Join(root, "api", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		path string
		ok   bool
	}{
		"relative escape":  {"../o.png", false},
		"not an image":     {"api/notes.txt", false},
		"missing":          {"api/nope.png", false},
		"absolute outside": {outside, true},
	} {
		_, err := resolveImage(root, tc.path)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A message from an agent never attaches.
	e := h.rt.agents["scout"]
	if got, err := h.rt.turnImages(e, bus.Message{Author: "other", Text: "@image:" + outside}); got != nil || err != nil {
		t.Fatalf("agent mention must be ignored: %v %v", got, err)
	}
}
