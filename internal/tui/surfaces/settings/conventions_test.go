package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

func TestConventionsTemplatesSection(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.sec = secConventions
	view := ansi.Strip(m.View())
	for _, want := range []string{"CONVENTIONS", "default", "feature", "ticket", "personal", "PREVIEW default",
		"branch    task/login-timeout", "worktree  .dhi/tasks/login-timeout/api"} {
		if !strings.Contains(view, want) {
			t.Fatalf("section missing %q:\n%s", want, view)
		}
	}
	golden.Snapshot(t, "settings_conventions", view)

	// Use the ticket template: rules applied and saved to the tracked file.
	m.convCur = 2
	feed(m, "enter")
	if m.cfg.Conventions.Active != "ticket" || m.cfg.Conventions.Branch.Task != "{ticket}/{slug}" {
		t.Fatalf("not applied: %+v", m.cfg.Conventions)
	}
	path := filepath.Join(ws.Root, ".dhi", settings.ConventionsFile)
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), `active = "ticket"`) {
		t.Fatalf("not saved to %s: %v\n%s", path, err, data)
	}

	// Built-ins are fixed.
	feed(m, "e")
	if m.dlg != nil || !strings.Contains(m.flash, "built-in templates are fixed") {
		t.Fatalf("edit of a built-in: dlg=%v flash=%q", m.dlg != nil, m.flash)
	}

	// Copy it into a team template, with a broken worktree first.
	feed(m, "n")
	typeDialog(m, "team")
	m.dform.Fields[4].Value = "{slug}"
	feed(m, "enter")
	if m.dlg == nil || !strings.Contains(m.dform.Err, "{member}") {
		t.Fatalf("bad worktree accepted (err %q)", m.dform.Err)
	}
	m.dform.Fields[4].Value = "{ticket}-{member}"
	m.dform.Fields[2].Value = "work/{ticket}/{slug}"
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("dialog still open: %q", m.dform.Err)
	}
	tpl, builtin, ok := m.cfg.Conventions.Lookup("team")
	if !ok || builtin || tpl.Branch.Task != "work/{ticket}/{slug}" || tpl.Worktree.Task != "{ticket}-{member}" {
		t.Fatalf("team template = %+v ok=%v", tpl, ok)
	}
	// It reloads from disk as written.
	cfg, err := settings.Load("", path)
	if err != nil {
		t.Fatalf("saved conventions do not load: %v", err)
	}
	if _, _, ok := cfg.Conventions.Lookup("team"); !ok {
		t.Fatal("team template lost on reload")
	}

	// Select and delete it.
	for i, n := range m.convNames() {
		if n == "team" {
			m.convCur = i
		}
	}
	feed(m, "d")
	feed(m, "enter")
	if _, _, ok := m.cfg.Conventions.Lookup("team"); ok {
		t.Fatal("team template not deleted")
	}
}

// Both writers share one file and patch only their keys: a CONFIG row
// save neither reverts a template nor copies it elsewhere, and a deleted
// template stays deleted after a reload.
func TestConventionWritersDoNotRevertEachOther(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	path := filepath.Join(ws.Root, ".dhi", settings.ConventionsFile)
	if err := os.WriteFile(path, []byte("[conventions.copyright]\nholder = \"Acme\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.sec = secConventions
	feed(m, "n")
	typeDialog(m, "team")
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("template not saved: %q", m.dform.Err)
	}
	m.sec = secConfig
	m.cursor = rowCommitFormat
	feed(m, "l") // a CONFIG row save
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `holder = "Acme"`) || !strings.Contains(string(data), "[conventions.templates.team") {
		t.Fatalf("CONFIG save rewrote other keys:\n%s", data)
	}
	m.sec = secConventions
	for i, n := range m.convNames() {
		if n == "team" {
			m.convCur = i
		}
	}
	feed(m, "d")
	feed(m, "enter")
	cfg, err := settings.Load(m.savePath, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cfg.Conventions.Lookup("team"); ok {
		t.Fatal("deleted template came back on reload")
	}
	if cfg.Conventions.Commit.Format != "conventional" || cfg.Conventions.Copyright.Holder != "Acme" {
		t.Fatalf("reloaded = %+v", cfg.Conventions)
	}
}

// The co-author is personal: it never lands in the tracked team file.
func TestCoAuthorStaysOutOfTheTeamFile(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.savePath = filepath.Join(t.TempDir(), "config.toml")
	m.cfg.Conventions.Commit.CoAuthor = "Dana <dana@example.com>"
	m.sec = secConfig
	m.cursor = rowCommitCoAuthor
	feed(m, "enter")
	if b, err := os.ReadFile(filepath.Join(ws.Root, ".dhi", settings.ConventionsFile)); err == nil && strings.Contains(string(b), "dana@") {
		t.Fatalf("co-author written to the team file:\n%s", b)
	}
	b, err := os.ReadFile(settings.ConventionsPath(m.savePath))
	if err != nil || !strings.Contains(string(b), "dana@example.com") {
		t.Fatalf("co-author not in the user file: %v\n%s", err, b)
	}
}

func TestConventionsLearnFromExample(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.sec = secConventions
	feed(m, "p")
	if m.dkind != dlgConvExample {
		t.Fatal("p did not open the example form")
	}
	feed(m, "enter")
	if m.dform == nil || !strings.Contains(m.dform.Err, "paste a commit") {
		t.Fatalf("empty example accepted: %+v", m.dform)
	}
	for _, r := range `feat(api): add login\n\nWhy: users asked.\nRisk: low` {
		m.HandleKey(string(r))
	}
	feed(m, "enter")
	if m.dkind != dlgConvLearn {
		t.Fatal("no confirmation after the example")
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Conventional Commit") || !strings.Contains(v, "Why: users asked. ⏎ Risk: low") {
		t.Fatalf("confirmation:\n%s", v)
	}
	golden.Snapshot(t, "settings_conventions_learn", m.View())
	feed(m, "enter")
	c := m.cfg.Conventions
	if c.Commit.Format != conventions.FormatConventional || c.Commit.Body != "Why: users asked.\nRisk: low" {
		t.Fatalf("not applied: %+v", c.Commit)
	}
	data, _ := os.ReadFile(filepath.Join(ws.Root, ".dhi", settings.ConventionsFile))
	if !strings.Contains(string(data), `format = "conventional"`) || !strings.Contains(string(data), "Risk: low") {
		t.Fatalf("not saved:\n%s", data)
	}
}

func TestConventionsLearnFromGit(t *testing.T) {
	m, _, _, _ := agentSurface(t)
	m.sec = secConventions
	feed(m, "g")
	if !strings.Contains(m.flash, "unavailable") {
		t.Fatalf("flash %q", m.flash)
	}
	m.d.LearnConventions = func() conventions.Inference { return conventions.Inference{} }
	feed(m, "g")
	if !strings.Contains(m.flash, "not enough history") {
		t.Fatalf("flash %q", m.flash)
	}
	m.d.LearnConventions = func() conventions.Inference {
		return conventions.InferCommits([]string{"PROJ-1: a", "PROJ-2: b", "PROJ-3: c", "PROJ-4: d", "PROJ-5: e"})
	}
	feed(m, "g", "enter")
	if c := m.cfg.Conventions.Commit; c.Format != conventions.FormatTicket || c.Subject != "{ticket}: {subject}" {
		t.Fatalf("not applied: %+v", c)
	}
}
