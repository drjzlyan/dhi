package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

func pairFile(t *testing.T, content string) (*Model, string) {
	t.Helper()
	m := newEditor(t)
	abs := filepath.Join(m.members[0].path, "a.go")
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := m.OpenPaths(abs); n != 1 {
		t.Fatalf("OpenPaths opened %d", n)
	}
	m.Resize(120, 30)
	return m, abs
}

func TestContextReportsCursorSelectionAndLines(t *testing.T) {
	m, _ := pairFile(t, "alpha\nbeta\ngamma\n")
	e := m.active()
	e.Buffer().SetCursor(textbuf.Pos{Line: 1, Col: 1})
	out, err := m.Context()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"file: ", "cursor: line 1 col 1", "mode: normal", ">   1| beta", "    0| alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("context missing %q:\n%s", want, out)
		}
	}
	// A visual selection is reported with its text.
	m.HandleKey("v")
	m.HandleKey("l")
	m.HandleKey("l")
	out, _ = m.Context()
	if !strings.Contains(out, "mode: visual") || !strings.Contains(out, "selection: line 1 col 1") || !strings.Contains(out, "<<<\neta\n>>>") {
		t.Fatalf("selection not reported:\n%s", out)
	}
}

func TestContextWithoutBufferRefuses(t *testing.T) {
	m := newEditor(t)
	if _, err := m.Context(); err == nil || !strings.Contains(err.Error(), "no buffer") {
		t.Fatalf("err = %v", err)
	}
}

func TestProposeValidatesAgainstLiveBuffer(t *testing.T) {
	m, abs := pairFile(t, "foo bar\nfoo baz\n")
	cases := []struct {
		name, old, new, want string
	}{
		{"missing", "nope", "x", "not found"},
		{"ambiguous", "foo", "x", "appears 2 times"},
		{"no-op", "bar", "bar", "changes nothing"},
		{"empty old", "", "x", "required"},
	}
	for _, c := range cases {
		if err := m.Propose(abs, c.old, c.new, "", "ana"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if m.ProposalCount() != 0 {
		t.Fatal("refused proposals must not queue")
	}
	if err := m.Propose(abs, "bar", "qux", "clearer name", "ana"); err != nil {
		t.Fatal(err)
	}
	if err := m.Propose(abs, "bar", "qux", "", "ana"); err == nil || !strings.Contains(err.Error(), "identical") {
		t.Fatalf("duplicate err = %v", err)
	}
}

func TestReviewAcceptAppliesAsOneUndoStep(t *testing.T) {
	m, abs := pairFile(t, "foo bar\n")
	if err := m.Propose(abs, "bar", "qux", "rename", "ana"); err != nil {
		t.Fatal(err)
	}
	out := ansi.Strip(m.View())
	for _, want := range []string{"ana suggests a change", "- bar", "+ qux", "rename", "y accept"} {
		if !strings.Contains(out, want) {
			t.Errorf("review view missing %q:\n%s", want, out)
		}
	}
	if ctx, label := m.StatusContext(); ctx != "suggestion" || label != "REVIEW" {
		t.Fatalf("status = %q/%q", ctx, label)
	}
	// Keys belong to the overlay: a stray 'x' must not edit the buffer.
	if !m.HandleKey("y") {
		t.Fatal("y not consumed")
	}
	if got := m.active().Buffer().Text(); got != "foo qux\n" {
		t.Fatalf("buffer = %q", got)
	}
	if m.ProposalCount() != 0 || m.reviewOpen {
		t.Fatal("accepted proposal should clear the overlay")
	}
	m.HandleKey("u")
	if got := m.active().Buffer().Text(); got != "foo bar\n" {
		t.Fatalf("one undo should restore, got %q", got)
	}
}

func TestReviewRejectAndDefer(t *testing.T) {
	m, abs := pairFile(t, "foo bar\n")
	_ = m.Propose(abs, "bar", "qux", "", "ana")
	m.HandleKey("esc") // decide later
	if m.reviewOpen || m.ProposalCount() != 1 {
		t.Fatalf("esc should defer, open=%v n=%d", m.reviewOpen, m.ProposalCount())
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "1 suggestion(s)") {
		t.Fatalf("title chip missing:\n%s", out)
	}
	m.HandleKey("ctrl+y")
	if !m.reviewOpen {
		t.Fatal("ctrl+y should reopen the review")
	}
	m.HandleKey("n")
	if m.ProposalCount() != 0 || m.active().Buffer().Text() != "foo bar\n" {
		t.Fatal("reject must leave the buffer untouched")
	}
}

func TestReviewStaleProposalIsDroppedNamed(t *testing.T) {
	m, abs := pairFile(t, "foo bar\n")
	_ = m.Propose(abs, "bar", "qux", "", "ana")
	// The human edits the target away before accepting.
	if _, err := m.active().Buffer().ReplaceText("bar", "zzz", false); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("y")
	if m.ProposalCount() != 0 {
		t.Fatal("stale proposal must be dropped")
	}
	if !strings.Contains(m.pairNote, "stale") {
		t.Fatalf("note = %q", m.pairNote)
	}
	if got := m.active().Buffer().Text(); got != "foo zzz\n" {
		t.Fatalf("buffer = %q", got)
	}
}

func TestAcceptAllAppliesEveryProposal(t *testing.T) {
	m, abs := pairFile(t, "one\ntwo\nthree\n")
	_ = m.Propose(abs, "one", "1", "", "ana")
	_ = m.Propose(abs, "two", "2", "", "ana")
	_ = m.Propose(abs, "three", "3", "", "ana")
	m.HandleKey("A")
	if got := m.active().Buffer().Text(); got != "1\n2\n3\n" {
		t.Fatalf("buffer = %q", got)
	}
}

func pairChatEditor(t *testing.T) (*chatHarness, string) {
	t.Helper()
	h := newChatEditor(t, "ok")
	abs := filepath.Join(h.m.members[0].path, "a.go")
	if err := os.WriteFile(abs, []byte("func f() {}\nfunc g() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := h.m.OpenPaths(abs); n != 1 {
		t.Fatalf("OpenPaths opened %d", n)
	}
	return h, abs
}

func humanPosts(h *chatHarness, ch string) []string {
	var out []string
	for _, msg := range h.rt.Bus().History(ch, 0) {
		if msg.Author == "you" {
			out = append(out, msg.Text)
		}
	}
	return out
}

func TestPairCommandInvitesAgentAndAsksAboutSelection(t *testing.T) {
	h, _ := pairChatEditor(t)
	typeKeys(h.m, ":pair scout")
	h.m.HandleKey("enter")
	if h.m.pairAgent != "scout" {
		t.Fatalf("pairAgent = %q, msg=%q", h.m.pairAgent, h.m.active().Message())
	}
	posts := humanPosts(h, "dm:scout")
	if len(posts) != 1 || !strings.Contains(posts[0], "editor_context") || !strings.Contains(posts[0], "editor_propose_edit") {
		t.Fatalf("kickoff = %q", posts)
	}
	if out := ansi.Strip(h.m.View()); !strings.Contains(out, "pairing: scout") {
		t.Fatalf("pairing badge missing:\n%s", out)
	}

	// Select line 0 and ask about it: the code rides inline.
	h.m.HandleKey("v")
	for i := 0; i < 5; i++ { // "func " on line 0
		h.m.HandleKey("l")
	}
	typeKeys(h.m, ":ask why is f empty?")
	h.m.HandleKey("enter")
	posts = humanPosts(h, "dm:scout")
	last := posts[len(posts)-1]
	if !strings.Contains(last, "why is f empty?") || !strings.Contains(last, "lines 0-0") || !strings.Contains(last, "func f") {
		t.Fatalf("ask = %q", last)
	}

	typeKeys(h.m, ":unpair")
	h.m.HandleKey("enter")
	if h.m.pairAgent != "" {
		t.Fatal("unpair must end the session")
	}
}

func TestPairCommandRefusals(t *testing.T) {
	h, _ := pairChatEditor(t)
	for _, c := range []struct{ cmd, want string }{
		{":pair ghost", "unknown agent ghost"},
		{":pair", "usage: :pair"},
		{":ask hello", "start a session first"},
		{":unpair", "not pairing"},
	} {
		typeKeys(h.m, c.cmd)
		h.m.HandleKey("enter")
		if got := h.m.active().Message(); !strings.Contains(got, c.want) {
			t.Errorf("%s → %q, want %q", c.cmd, got, c.want)
		}
	}
	// No crew attached at all.
	m, _ := pairFile(t, "x\n")
	typeKeys(m, ":pair scout")
	m.HandleKey("enter")
	if got := m.active().Message(); !strings.Contains(got, "no crew") {
		t.Fatalf("no-crew msg = %q", got)
	}
}
