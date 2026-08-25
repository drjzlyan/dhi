package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func newSurface(t *testing.T) (*Model, *workspace.Workspace) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	repoA := filepath.Join(root, "repos", "alpha")
	repoB := filepath.Join(root, "beta")
	for _, dir := range []string{repoA, repoB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "schema = 1\n\n[members.alpha]\npath = \"repos/alpha\"\n\n[members.beta]\npath = \"beta\"\n"
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := New("0.1.0", ws, Deps{})
	m.Resize(110, 34)
	return m, ws
}

func TestMetaIsBootSurface(t *testing.T) {
	m, _ := newSurface(t)
	if got := m.Meta(); got.ID != "workspace" || got.Title != "Workspace" {
		t.Fatalf("meta = %+v", got)
	}
	var _ surfaces.Surface = m
}

func TestNilWorkspaceRendersHeroAndSwallowsKeys(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("0.1.0", nil, Deps{})
	m.Resize(100, 30)
	out := m.View()
	if !strings.Contains(out, "███████") || !strings.Contains(out, "not inside a DHI workspace") {
		t.Fatalf("empty state missing hero/hint:\n%s", out)
	}
	for _, k := range []string{"a", "r", "d", "j", "k", "[", "]"} {
		if m.HandleKey(k) {
			t.Fatalf("key %q consumed without a workspace", k)
		}
	}
}

func TestSectionCyclingWraps(t *testing.T) {
	m, _ := newSurface(t)
	if m.sec != secMembers {
		t.Fatalf("initial section = %v", m.sec)
	}
	m.HandleKey("[")
	if m.sec != secTasks {
		t.Fatalf("[ from first should wrap to tasks, got %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secMembers {
		t.Fatalf("] did not wrap back: %v", m.sec)
	}
	m.HandleKey("]")
	m.HandleKey("]")
	if m.sec != secPacks {
		t.Fatalf("two ] from members should reach packs, got %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secStandards {
		t.Fatalf("third ] should reach standards, got %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secChannels {
		t.Fatalf("fourth ] should reach channels, got %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secTasks {
		t.Fatalf("fifth ] should reach tasks, got %v", m.sec)
	}
}

func TestViewRendersAllSectionsAndBounds(t *testing.T) {
	m, _ := newSurface(t)
	out := ansi.Strip(m.View())
	for _, want := range []string{"MEMBERS", "ORG", "PACKS", "STANDARDS", "alpha"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
	for i, l := range strings.Split(m.View(), "\n") {
		if w := len([]rune(ansi.Strip(l))); w > 110 {
			t.Fatalf("line %d exceeds width: %d", i, w)
		}
	}
}

// typeInto focuses form field idx (replacing its contents) and types s.
func typeInto(t *testing.T, m *Model, idx int, s string) {
	t.Helper()
	for i := 0; i < len(m.form.fields)*2 && m.form.cur != idx; i++ {
		m.HandleKey("tab")
	}
	if m.form.cur != idx {
		t.Fatalf("cannot reach field %d (cur=%d)", idx, m.form.cur)
	}
	m.form.fields[idx].runes = nil // replace, not append to prefills
	for _, r := range s {
		if !m.HandleKey(string(r)) {
			t.Fatalf("field %d rejected rune %q", idx, string(r))
		}
	}
}

func TestAddMemberLocalPathFlow(t *testing.T) {
	m, ws := newSurface(t)
	gamma := filepath.Join(ws.Root, "gamma")
	os.MkdirAll(gamma, 0o755)

	m.HandleKey("a")
	typeInto(t, m, 0, "gamma")
	typeInto(t, m, 1, gamma)
	m.HandleKey("enter")

	if _, ok := ws.Member("gamma"); !ok {
		t.Fatal("gamma not registered after form submit")
	}
	if m.form.kind != fNone {
		t.Fatalf("modal still open: %+v err=%q", m.form.kind, m.form.err)
	}
}

func TestRenameMemberFlow(t *testing.T) {
	m, ws := newSurface(t)
	m.HandleKey("j") // beta
	m.HandleKey("r")
	m.form.fields[0].runes = []rune("aab")
	m.HandleKey("enter")
	if _, ok := ws.Member("aab"); !ok {
		t.Fatalf("rename failed: %+v err=%q", ws.Members(), m.form.err)
	}
	if _, ok := ws.Member("beta"); ok {
		t.Fatal("old alias still present")
	}
}

func TestRemoveMemberConfirmKeepsTree(t *testing.T) {
	m, ws := newSurface(t)
	m.HandleKey("j")
	m.HandleKey("d")
	m.HandleKey("enter")
	if _, ok := ws.Member("beta"); ok {
		t.Fatal("beta still registered")
	}
	if info, err := os.Stat(filepath.Join(ws.Root, "beta")); err != nil || !info.IsDir() {
		t.Errorf("working tree must survive: %v", err)
	}
	// Last-member guard surfaces inline and keeps the modal open.
	m.HandleKey("d")
	m.HandleKey("enter")
	if m.form.kind == fNone || !strings.Contains(m.form.err, "last member") {
		t.Fatalf("guard = kind:%v err:%q", m.form.kind, m.form.err)
	}
}

func TestTeamCreateEditDeleteFlow(t *testing.T) {
	m, _ := newSurface(t)
	m.HandleKey("]") // org

	m.HandleKey("t")
	typeInto(t, m, 0, "frontend")
	typeInto(t, m, 1, "you")
	typeInto(t, m, 2, "alice,bob,alice")
	m.HandleKey("enter")

	tm, ok := m.org.Team("frontend")
	if !ok || tm.Lead != "you" || strings.Join(tm.Members, ",") != "alice,bob" {
		t.Fatalf("team after create = %+v err=%q", tm, m.form.err)
	}

	// Cursor sits on the only row; edit it.
	m.HandleKey("enter")
	if m.form.kind != fTeamEdit || m.form.orig != "frontend" {
		t.Fatalf("edit modal = %+v orig=%q", m.form.kind, m.form.orig)
	}
	typeInto(t, m, 1, "alice")
	typeInto(t, m, 2, "alice,zoe")
	m.HandleKey("enter")
	tm, _ = m.org.Team("frontend")
	if tm.Lead != "alice" || strings.Join(tm.Members, ",") != "alice,zoe" {
		t.Fatalf("team after edit = %+v", tm)
	}

	// Delete with confirmation.
	m.cursors[secOrg] = 0
	m.HandleKey("x")
	if m.form.kind != fTeamDeleteConfirm {
		t.Fatalf("expected delete confirm, got %v", m.form.kind)
	}
	m.HandleKey("esc")
	if _, ok := m.org.Team("frontend"); !ok {
		t.Fatal("esc deleted anyway")
	}
	m.HandleKey("x")
	m.HandleKey("enter")
	if _, ok := m.org.Team("frontend"); ok {
		t.Fatal("delete not applied")
	}
}

func writeAgentManifest(t *testing.T, dir, id string) {
	t.Helper()
	doc := "schema = 1\nname = \"" + strings.Title(id) + "\"\nmodel = \"mock-1\"\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentCreateArchiveRestoreFlow(t *testing.T) {
	m, ws := newSurface(t)
	rosterDir := filepath.Join(ws.Root, ".dhi", "agents")
	writeAgentManifest(t, rosterDir, "alice")

	m.HandleKey("]") // org; cursor 0 = no teams yet → crew rows start here
	// Row 0 is alice (no teams exist).
	if _, active, _ := m.orgRows(); len(active) != 1 || active[0] != "alice" {
		t.Fatalf("crew rows = %v", active)
	}
	m.HandleKey("x")
	if m.form.kind != fAgentArchiveConfirm {
		t.Fatalf("expected archive confirm, got %v", m.form.kind)
	}
	m.HandleKey("enter")
	if _, active, archived := m.orgRows(); len(active) != 0 || len(archived) != 1 {
		t.Fatalf("after archive: active=%v archived=%v", active, archived)
	}
	if info, err := os.Stat(filepath.Join(rosterDir, ".archived", "alice.toml")); err != nil || info.IsDir() {
		t.Fatalf("archived manifest missing: %v", err)
	}

	// Cursor still lands on a selectable row; restore it.
	c := m.cursors[secOrg]
	_, active, archived := m.orgRows()
	if c < len(active) || c >= len(active)+len(archived) {
		m.cursors[secOrg] = len(active) // point at first archived row
	}
	m.HandleKey("R")
	if _, active, _ = m.orgRows(); len(active) != 1 || active[0] != "alice" {
		t.Fatalf("restore failed: %v", active)
	}

	// New-agent modal creates a valid manifest through strict Marshal.
	m.HandleKey("A")
	typeInto(t, m, 0, "bob")
	typeInto(t, m, 1, "Bob")
	typeInto(t, m, 2, "mock-1")
	typeInto(t, m, 3, "Be brief.")
	m.HandleKey("enter")
	if m.form.err != "" {
		t.Fatalf("agent create error: %q", m.form.err)
	}
	if _, err := os.Stat(filepath.Join(rosterDir, "bob.toml")); err != nil {
		t.Fatalf("bob.toml missing: %v", err)
	}
}

func fixturePackDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	agents := filepath.Join(root, "pack", "agents")
	os.MkdirAll(agents, 0o755)
	os.WriteFile(filepath.Join(agents, "carol.toml"),
		[]byte("schema = 1\nname = \"Carol\"\nmodel = \"mock-1\"\ntools = [\"read\"]\n"), 0o644)
	os.WriteFile(filepath.Join(root, "pack", "pack.toml"),
		[]byte("schema = 1\nname = \"acme\"\nversion = \"0.1.0\"\nagents = [\"agents/carol.toml\"]\n"), 0o644)
	return filepath.Join(root, "pack")
}

func TestPackInstallAndUninstallFlow(t *testing.T) {
	m, ws := newSurface(t)
	src := fixturePackDir(t)

	m.HandleKey("]") // org
	m.HandleKey("]") // packs
	if m.sec != secPacks {
		t.Fatalf("section = %v", m.sec)
	}
	m.HandleKey("i")
	typeInto(t, m, 0, src)
	m.HandleKey("enter")
	if !m.form.busy {
		t.Fatal("install not marked busy")
	}

	// Pump the async result like the program loop would.
	select {
	case ev := <-m.events:
		m.Update(ev)
	case <-time.After(3 * time.Second):
		t.Fatal("no install event")
	}
	if m.form.kind != fNone || m.form.flash == "" {
		t.Fatalf("form after install = kind:%v flash:%q err:%q",
			m.form.kind, m.form.flash, m.form.err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, ".dhi", "agents", "carol.toml")); err != nil {
		t.Fatalf("carol not installed: %v", err)
	}

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "acme") || !strings.Contains(out, "0.1.0") {
		t.Fatalf("pack listing missing:\n%s", out)
	}

	m.HandleKey("x")
	if m.form.kind != fPackUninstallConfirm {
		t.Fatalf("expected uninstall confirm, got %v", m.form.kind)
	}
	m.HandleKey("enter")
	if _, err := os.Stat(filepath.Join(ws.Root, ".dhi", "agents", "carol.toml")); !os.IsNotExist(err) {
		t.Fatalf("carol survived uninstall: %v", err)
	}
}

func TestStandardsLayersFlow(t *testing.T) {
	m, ws := newSurface(t)
	rosterDir := filepath.Join(ws.Root, ".dhi", "agents")
	writeAgentManifest(t, rosterDir, "alice")
	m.org.CreateTeam("frontend", "", []string{"alice"})

	m.HandleKey("]")
	m.HandleKey("]") // packs
	m.HandleKey("]") // standards
	if m.sec != secStandards {
		t.Fatalf("section = %v", m.sec)
	}

	// Workspace layer via w.
	m.HandleKey("w")
	typeInto(t, m, 0, "use conventional commits, run lint")
	m.HandleKey("enter")
	snap, err := inspectFor(m)
	if err != nil || strings.Join(snap.Workspace, "|") != "use conventional commits|run lint" {
		t.Fatalf("workspace layer = %+v err=%v", snap, err)
	}

	// Team layer: cursor row 0=workspace, 1=team frontend.
	m.cursors[secStandards] = 1
	m.HandleKey("t")
	if m.form.orig != "@team:frontend" {
		t.Fatalf("target = %q", m.form.orig)
	}
	typeInto(t, m, 0, "prefer table-driven tests")
	m.HandleKey("enter")
	snap, _ = inspectFor(m)
	if strings.Join(snap.Teams["frontend"], "|") != "prefer table-driven tests" {
		t.Fatalf("team layer = %+v", snap.Teams)
	}

	// Agent override with replace mode: cursor on agent row (index 2).
	m.cursors[secStandards] = 2
	m.HandleKey("g")
	if m.form.orig != "@agent:alice" || m.form.fields[0].text() != "alice" {
		t.Fatalf("g prefill = %q / %q", m.form.orig, m.form.fields[0].text())
	}
	// Mode field: tab once, flip to replace.
	m.HandleKey("tab")
	m.HandleKey("right")
	if got := m.form.fields[1].toggleValue(); got != "replace" {
		t.Fatalf("mode = %q", got)
	}
	typeInto(t, m, 2, "diffs only")
	m.HandleKey("enter")
	snap, _ = inspectFor(m)
	ov, ok := snap.Agents["alice"]
	if !ok || ov.Mode != "replace" || strings.Join(ov.Entries, "|") != "diffs only" {
		t.Fatalf("agent override = %+v", ov)
	}

	// Preview shows resolved block including builtins but not dropped layers.
	m.HandleKey("v")
	m.HandleKey("enter")
	if m.form.kind != fStdPreviewShow || len(m.form.preview) == 0 {
		t.Fatalf("preview = %v lines=%d", m.form.kind, len(m.form.preview))
	}
	text := strings.Join(m.form.preview, "\n")
	for _, want := range []string{"diffs only", "force-push"} {
		if !strings.Contains(text, want) {
			t.Errorf("preview missing %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"conventional commits", "table-driven"} {
		if strings.Contains(text, gone) {
			t.Errorf("replace leaked %q", gone)
		}
	}
}

func inspectFor(m *Model) (*stdSnapshotAlias, error) {
	return inspectSnapshot(m.ws.Root)
}

func TestFormEscSwallowWhileBusy(t *testing.T) {
	m, _ := newSurface(t)
	m.HandleKey("]")
	m.HandleKey("]")
	m.HandleKey("i")
	m.form.busy = true
	if !m.HandleKey("x") || !m.HandleKey("j") {
		t.Fatal("busy form must swallow keys")
	}
	m.form.busy = false
	m.HandleKey("esc")
	if m.form.kind != fNone {
		t.Fatal("esc did not close idle form")
	}
}

func TestTasksSectionFlows(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	var detached []string
	store.SetAttach(
		func(slug, member, branch, sp string) (string, error) {
			rel := ".dhi/tasks/" + slug + "/" + member
			os.MkdirAll(filepath.Join(ws.Root, rel), 0o755)
			return rel, nil
		},
		func(slug, rel string) error {
			detached = append(detached, rel)
			return nil
		},
	)
	m.taskStore = store

	// Navigate to the last section.
	for i := secMembers; i < secTasks; i++ {
		m.HandleKey("]")
	}
	if m.sec != secTasks {
		t.Fatalf("section = %v", m.sec)
	}

	// Create via modal (slug+title).
	m.HandleKey("n")
	typeInto(t, m, 0, "fix-login")
	typeInto(t, m, 1, "Fix login race")
	m.HandleKey("enter")
	if tk, ok := store.Get("fix-login"); !ok || tk.Status != tasks.Backlog {
		t.Fatalf("card after create = %+v err=%q", tk, m.form.err)
	}

	// Status cycles backlog → active.
	m.HandleKey("s")
	tk, _ := store.Get("fix-login")
	if tk.Status != tasks.Active {
		t.Fatalf("status = %v", tk.Status)
	}

	// Assign via modal.
	m.HandleKey("a")
	typeInto(t, m, 0, "alice")
	m.HandleKey("enter")
	tk, _ = store.Get("fix-login")
	if tk.Assignee != "alice" {
		t.Fatalf("assignee = %q", tk.Assignee)
	}

	// Attach worktree through the fake seam.
	m.HandleKey("w")
	typeInto(t, m, 0, "alpha") // member from fixture workspace
	m.HandleKey("enter")
	tk, _ = store.Get("fix-login")
	if len(tk.ChangeSets) != 1 || tk.ChangeSets[0].Member != "alpha" ||
		tk.ChangeSets[0].Branch != "task/fix-login" {
		t.Fatalf("changesets = %+v err=%q", tk.ChangeSets, m.form.err)
	}
	if info, err2 := os.Stat(filepath.Join(ws.Root, tk.ChangeSets[0].Path)); err2 != nil || !info.IsDir() {
		t.Fatalf("fake worktree missing: %v", err2)
	}

	// Bind thread.
	m.HandleKey("t")
	typeInto(t, m, 0, "#general")
	m.form.fields[1].runes = []rune("42")
	m.HandleKey("enter")
	tk, _ = store.Get("fix-login")
	if tk.ThreadChannel != "#general" || tk.ThreadID != 42 {
		t.Fatalf("thread binding = %+v", tk)
	}

	// Detail line renders changeset + thread for the selected row.
	out := ansi.Strip(m.View())
	for _, want := range []string{"alpha@task/fix-login", "thread #general#42", "active"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}

	// Remove confirm deletes the card but leaves the worktree dir.
	m.HandleKey("x")
	if m.form.kind != fTaskRemoveConfirm {
		t.Fatalf("expected remove confirm, got %v", m.form.kind)
	}
	m.HandleKey("enter")
	if _, ok := store.Get("fix-login"); ok {
		t.Fatal("card survived removal")
	}
	if len(detached) != 0 {
		t.Fatalf("remove must not detach silently: %v", detached)
	}
	if _, err2 := os.Stat(filepath.Join(ws.Root, ".dhi/tasks/fix-login")); err2 != nil {
		t.Error("worktree dir was removed by card delete")
	}
}

func TestTasksSectionWithoutStoreRendersUnavailable(t *testing.T) {
	m, _ := newSurface(t)
	for i := secMembers; i < secTasks; i++ {
		m.HandleKey("]")
	}
	m.HandleKey("n") // must not open a modal without a store
	if m.form.kind == fTaskNew {
		t.Fatal("modal opened without task store")
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "task store unavailable") {
		t.Fatalf("unavailable note missing:\n%s", out)
	}
}
