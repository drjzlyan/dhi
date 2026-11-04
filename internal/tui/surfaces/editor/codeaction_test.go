package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func editFor(uri, text string) map[string]any {
	return map[string]any{"changes": map[string]any{uri: []map[string]any{{
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 0, "character": 0},
		}, "newText": text}}}}
}

func openWithCfg(t *testing.T, cfg func(*goplusFake)) (*Model, *goplusFake) {
	t.Helper()
	srv, mgr := startFakeServerCfg(t, "go", false, cfg)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")
	return m, srv
}

func TestRankActionsQuickfixFirstDropsDisabled(t *testing.T) {
	got := rankActions([]lsp.CodeAction{
		{Title: "extract", Kind: "refactor.extract"},
		{Title: "organize", Kind: "source.organizeImports"},
		{Title: "off", Kind: "quickfix", Disabled: "no"},
		{Title: "fix2", Kind: "quickfix"},
		{Title: "fix1", Kind: "quickfix", Preferred: true},
	})
	var titles []string
	for _, a := range got {
		titles = append(titles, a.Title)
	}
	if strings.Join(titles, ",") != "fix1,fix2,extract,organize" {
		t.Errorf("order = %v", titles)
	}
}

func TestLightbulbShowsOnQuickFixLine(t *testing.T) {
	m, srv := openWithCfg(t, nil)
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), theme.GlyphBulb) }, "bulb")
	srv.mu.Lock()
	seen := strings.Join(srv.onlySeen, "|")
	srv.mu.Unlock()
	if !strings.Contains(seen, "quickfix") {
		t.Errorf("probe did not ask for quickfix only: %q", seen)
	}
}

func TestLightbulbAbsentWithoutQuickFix(t *testing.T) {
	m, srv := openWithCfg(t, func(f *goplusFake) { f.actionsFor = map[string][]map[string]any{"": {}} })
	waitFor(t, m, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.onlySeen) > 0
	}, "probe asked")
	m.drainTerm()
	if strings.Contains(plainView(m), theme.GlyphBulb) {
		t.Error("bulb shown with no action")
	}
}

func TestCodeActionPopupOrdersQuickfixFirst(t *testing.T) {
	m, _ := openWithCfg(t, func(f *goplusFake) {
		f.actionsFor = map[string][]map[string]any{"": {
			{"title": "extract thing", "kind": "refactor.extract", "edit": editFor("file:///x", "")},
			{"title": "the fix", "kind": "quickfix", "edit": editFor("file:///x", "")},
		}}
	})
	feed(m, "g", "a")
	waitFor(t, m, func() bool { return m.actionOpen }, "popup")
	if m.actionItems[0].Title != "the fix" {
		t.Errorf("first = %q", m.actionItems[0].Title)
	}
}

func TestVisualGAOpensActions(t *testing.T) {
	m, _ := openWithCfg(t, nil)
	feed(m, "v", "g", "a")
	waitFor(t, m, func() bool { return m.actionOpen }, "popup from visual")
}

func TestCodeActionResolveFillsEdit(t *testing.T) {
	m, srv := openWithCfg(t, func(f *goplusFake) {
		f.resolveCaps = true
		f.actionsFor = map[string][]map[string]any{"": {{"title": "lazy fix", "kind": "quickfix", "data": map[string]any{"id": 7}}}}
	})
	srv.mu.Lock()
	srv.resolved = map[string]any{"title": "lazy fix", "edit": editFor("file://"+m.active().Path(), "// resolved\n")}
	srv.mu.Unlock()
	feed(m, "g", "a")
	waitFor(t, m, func() bool { return m.actionOpen }, "popup")
	feed(m, "enter")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Buffer().Text(), "// resolved") }, "resolved edit applied")
	feed(m, "u")
	if strings.Contains(m.active().Buffer().Text(), "// resolved") {
		t.Error("one undo should revert the whole action")
	}
}

func TestCodeActionCommandAppliesServerEdit(t *testing.T) {
	m, srv := openWithCfg(t, func(f *goplusFake) {
		f.actionsFor = map[string][]map[string]any{"": {{"title": "run it", "kind": "quickfix",
			"command": map[string]any{"title": "run it", "command": "fake.run"}}}}
	})
	srv.mu.Lock()
	srv.execEdit = editFor("file://"+m.active().Path(), "// from command\n")
	srv.mu.Unlock()
	feed(m, "g", "a")
	waitFor(t, m, func() bool { return m.actionOpen }, "popup")
	feed(m, "enter")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Buffer().Text(), "// from command") }, "applyEdit from command")
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.execCommands) != 1 || srv.execCommands[0] != "fake.run" {
		t.Errorf("executeCommand = %v", srv.execCommands)
	}
}

func TestBareCommandActionDecodes(t *testing.T) {
	var a lsp.CodeAction
	if err := a.UnmarshalJSON([]byte(`{"title":"t","command":"c.id","arguments":[1]}`)); err != nil {
		t.Fatal(err)
	}
	if a.Command == nil || a.Command.Command != "c.id" || len(a.Command.Arguments) != 1 {
		t.Errorf("command = %+v", a.Command)
	}
}

func TestOrganizeImports(t *testing.T) {
	m, srv := openWithCfg(t, func(f *goplusFake) {
		f.actionsFor = map[string][]map[string]any{
			"source.organizeImports": {{"title": "Organize Imports", "kind": "source.organizeImports", "edit": editFor("file:///placeholder", "")}},
		}
	})
	srv.mu.Lock()
	srv.actionsFor["source.organizeImports"][0]["edit"] = editFor("file://"+m.active().Path(), "// organized\n")
	srv.mu.Unlock()
	runEx(m, "organize")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Buffer().Text(), "// organized") }, "organize applied")
}

func TestOrganizeImportsNoneOffered(t *testing.T) {
	m, _ := openWithCfg(t, func(f *goplusFake) {
		f.actionsFor = map[string][]map[string]any{"source.organizeImports": {}}
	})
	runEx(m, "organize")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "offers no organize-imports") }, "named none")
}

func TestOrganizeRestrictedUntilTrusted(t *testing.T) {
	m, _, _ := trustedEditor(t)
	if msg := m.organizeImports(); !strings.Contains(msg, "restricted: trust this folder to organize imports") {
		t.Errorf("organize = %q", msg)
	}
}
