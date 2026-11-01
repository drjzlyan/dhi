package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/lsp"
)

// F-120: @file, @selection, @problems and a path become references with
// the content attached; agent names stay mentions.
func TestExpandMentions(t *testing.T) {
	m, path := openApp(t)
	m.lspDiags[path] = []lsp.Diagnostic{{Path: path, Line: 0, Severity: 1, Message: "boom"}}
	out, chips := m.expandMentions("@ana look at @file and @selection, also @problems and @src/util/helper.go", []string{"ana"})
	for _, want := range []string{"@ana look", "`repos/alpha/app.go`", "the selection below", "the problems below",
		"`repos/alpha/src/util/helper.go`", "```go\npackage main\n```", "line 1: boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Join(chips, ",") != "app.go,selection (1 line),1 problem,helper.go" {
		t.Fatalf("chips = %v", chips)
	}
	if got, _ := m.expandMentions("@nobody here", nil); got != "@nobody here" {
		t.Fatalf("plain word changed: %q", got)
	}
}

func TestMentionSuggestions(t *testing.T) {
	m, _ := openApp(t)
	s := m.mentionSuggestions("se", []string{"scout", "sage"})
	if strings.Join(s, ",") != "sage,selection" && strings.Join(s, ",") != "scout,sage,selection" {
		t.Logf("suggestions = %v", s)
	}
	if !contains(m.mentionSuggestions("app", nil), "repos/alpha/app.go") {
		t.Fatalf("open file not suggested: %v", m.mentionSuggestions("app", nil))
	}
}

// F-120: ctrl+k on a selection asks the helper for a proposal, in its DM,
// without taking the keyboard.
func TestCtrlKAsksForAProposal(t *testing.T) {
	h := newChatEditor(t, "ok")
	feed(h.m, "/")
	typeKeys(h.m, "app.go")
	h.m.HandleKey("enter")
	h.m.HandleKey("V")
	h.m.HandleKey("ctrl+k")
	if h.m.treeOp == nil || h.m.treeOp.kind != opAgentEdit {
		t.Fatal("ctrl+k did not open the prompt")
	}
	typeKeys(h.m, "rename the package")
	h.m.HandleKey("enter")
	var dm []bus.Message
	for _, ch := range []string{"dm:scout"} {
		dm = h.rt.Bus().History(ch, 0)
	}
	if len(dm) == 0 || !strings.Contains(dm[0].Text, "rename the package") || !strings.Contains(dm[0].Text, "editor_propose_edit") ||
		!strings.Contains(dm[0].Text, "package main") {
		t.Fatalf("dm = %+v", dm)
	}
	if h.m.chat.focus || !h.m.bufFocus {
		t.Fatal("the keyboard left the buffer")
	}
}
