package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
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

// newSurfaceWithBus wires the chat pane so board→floor jumps resolve.
func newSurfaceWithBus(t *testing.T) (*Model, *workspace.Workspace, *bus.Bus) {
	t.Helper()
	m, ws := newSurface(t)
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.bus = b
	m.pane = newChatPane(b, nil, m.org)
	m.wireUnreadSeams()
	m.refreshPaneRail()
	return m, ws, b
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
	if !strings.Contains(out, "███████") || !strings.Contains(out, "not a DHI workspace") {
		t.Fatalf("empty state missing hero/hint:\n%s", out)
	}
	for _, k := range []string{"a", "r", "d", "j", "k", "[", "]"} {
		if m.HandleKey(k) {
			t.Fatalf("key %q consumed without a workspace", k)
		}
	}
}

func TestBoardIsTheLandingSection(t *testing.T) {
	m, _ := newSurface(t)
	if m.sec != secBoard {
		t.Fatalf("initial section = %v, want the board", m.sec)
	}
}

func TestStatuslineContextFollowsZoneAndMode(t *testing.T) {
	m, _ := newSurface(t)
	zone, mode := m.StatusContext()
	if zone != "board" || mode != "" {
		t.Fatalf("context = %q %q", zone, mode)
	}
	m.sec = secRepos
	m.HandleKey("a") // add-repo modal owns the keys
	zone, mode = m.StatusContext()
	if zone != "repos" || mode != "FORM" {
		t.Fatalf("modal context = %q %q", zone, mode)
	}
	m.HandleKey("esc")
	m.sec = secInbox
	if _, mode := m.StatusContext(); mode != "" {
		t.Fatalf("inbox mode = %q", mode)
	}
	if h := m.sectionHints(); len(h) == 0 {
		t.Fatal("board hints missing")
	}
}

func TestSectionCyclingWraps(t *testing.T) {
	m, _ := newSurface(t)
	if m.sec != secBoard {
		t.Fatalf("initial section = %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secChannels {
		t.Fatalf("first ] should reach channels, got %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secRepos {
		t.Fatalf("second ] should reach repos, got %v", m.sec)
	}
	m.HandleKey("]")
	if m.sec != secInbox {
		t.Fatalf("third ] should wrap to inbox, got %v", m.sec)
	}
	m.HandleKey("[")
	if m.sec != secRepos {
		t.Fatalf("[ from inbox should wrap back to repos, got %v", m.sec)
	}
}

func TestViewRendersAllSectionsAndBounds(t *testing.T) {
	m, _ := newSurface(t)
	out := ansi.Strip(m.View())
	for _, want := range []string{"INBOX", "BOARD", "CHANNELS", "REPOS", "backlog", "active", "in-review", "done"} {
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
	for i := 0; i < len(m.form.f.Fields)*2 && m.form.f.Cur() != idx; i++ {
		m.HandleKey("tab")
	}
	if m.form.f.Cur() != idx {
		t.Fatalf("cannot reach field %d (cur=%d)", idx, m.form.f.Cur())
	}
	m.form.f.Fields[idx].Value = "" // replace, not append to prefills
	for _, r := range s {
		if !m.HandleKey(string(r)) {
			t.Fatalf("field %d rejected rune %q", idx, string(r))
		}
	}
}

func TestAddRepoLocalPathFlow(t *testing.T) {
	m, ws := newSurface(t)
	m.sec = secRepos
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

func TestRenameRepoFlow(t *testing.T) {
	m, ws := newSurface(t)
	m.sec = secRepos
	m.HandleKey("j") // beta
	m.HandleKey("r")
	m.form.f.Fields[0].Value = "aab"
	m.HandleKey("enter")
	if _, ok := ws.Member("aab"); !ok {
		t.Fatalf("rename failed: %+v err=%q", ws.Members(), m.form.err)
	}
	if _, ok := ws.Member("beta"); ok {
		t.Fatal("old alias still present")
	}
}

func TestRemoveRepoConfirmKeepsTree(t *testing.T) {
	m, ws := newSurface(t)
	m.sec = secRepos
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

func TestFormEscSwallowWhileBusy(t *testing.T) {
	m, _ := newSurface(t)
	m.sec = secRepos
	m.HandleKey("a")
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

func TestBoardFlows(t *testing.T) {
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
	m.sec = secBoard

	// Create via modal (slug+title).
	m.HandleKey("n")
	typeInto(t, m, 0, "fix-login")
	typeInto(t, m, 1, "Fix login race")
	m.HandleKey("enter")
	if tk, ok := store.Get("fix-login"); !ok || tk.Status != tasks.Backlog {
		t.Fatalf("card after create = %+v err=%q", tk, m.form.err)
	}

	// Status cycles backlog → active; the card moves lanes.
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
	m.form.f.Fields[1].Value = "42"
	m.HandleKey("enter")
	tk, _ = store.Get("fix-login")
	if tk.ThreadChannel != "#general" || tk.ThreadID != 42 {
		t.Fatalf("thread binding = %+v", tk)
	}

	// Detail pane renders assignee, changeset, thread, status for the
	// selected card.
	out := ansi.Strip(m.View())
	for _, want := range []string{"fix-login", "alice", "alpha@task/fix-login", "thread #general#42", "active"} {
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

func TestBoardWithoutStoreRendersUnavailable(t *testing.T) {
	m, _ := newSurface(t)
	m.HandleKey("n") // must not open a modal without a store
	if m.form.kind == fTaskNew {
		t.Fatal("modal opened without task store")
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "task store unavailable") {
		t.Fatalf("unavailable note missing:\n%s", out)
	}
}

func TestBoardLanesAndCursor(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard
	store.Create("a-one", "One", "", "")
	store.Create("b-two", "Two", "", "")
	if err := store.SetStatus("b-two", tasks.Active); err != nil {
		t.Fatal(err)
	}

	// Lane 0 holds the backlog card; l moves the active lane.
	if g := m.boardGroups(); len(g[0]) != 1 || len(g[1]) != 1 {
		t.Fatalf("groups = %+v", g)
	}
	m.HandleKey("l")
	if m.boardActive != 1 {
		t.Fatalf("active = %d", m.boardActive)
	}
	m.HandleKey("l")
	m.HandleKey("l") // clamped at done
	if m.boardActive != 3 {
		t.Fatalf("active = %d", m.boardActive)
	}
	m.HandleKey("h")
	m.HandleKey("h")
	m.HandleKey("h") // clamped at backlog
	if m.boardActive != 0 {
		t.Fatalf("active = %d", m.boardActive)
	}
	// Empty lane selection: action keys are not consumed (nothing selected).
	m.HandleKey("l")
	m.HandleKey("l")
	if m.HandleKey("s") {
		t.Fatal("s on an empty lane must not be consumed")
	}
	if len(store.List()) != 2 {
		t.Fatal("empty-lane keypress wrote to the store")
	}
}

func TestBoardOpenOnFloorJumps(t *testing.T) {
	m, ws, b := newSurfaceWithBus(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard

	// Bound thread jump.
	store.Create("with-thread", "Threaded", "", "")
	if err := store.BindThread("with-thread", "#general", 7); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("o")
	if m.sec != secChannels {
		t.Fatalf("thread jump landed in %v", m.sec)
	}
	if m.pane.threadID != 7 {
		t.Fatalf("thread pane = %d, want 7", m.pane.threadID)
	}

	// Assignee DM jump (the rail builds from the on-disk roster).
	rosterDir := filepath.Join(ws.Root, ".dhi", "agents")
	if err := os.MkdirAll(rosterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rosterDir, "scout.toml"),
		[]byte("schema = 1\nname = \"Scout\"\nmodel = \"m-1\"\nruntime = \"claude\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshPaneRail()
	store.Create("dm-jump", "Dmj", "scout", "")
	m.sec = secBoard
	m.boardActive = 0
	m.boardCur[0] = 0 // dm-jump sorts before lonely/with-thread
	m.HandleKey("o")
	if m.sec != secChannels {
		t.Fatalf("dm jump landed in %v", m.sec)
	}
	if m.pane.active == 0 || !strings.HasPrefix(m.pane.channels[m.pane.active], "dm:scout") {
		t.Fatalf("active channel = %q", m.pane.channels[m.pane.active])
	}

	// Neither → named flash, no jump.
	store.Create("lonely", "Lonely", "", "")
	m.sec = secBoard
	m.boardCur[0] = 1 // lonely
	m.HandleKey("o")
	if m.sec != secBoard {
		t.Fatal("nothing to open must not jump")
	}
	if m.form.err == "" || !strings.Contains(m.form.err, "no bound thread") {
		t.Fatalf("flash = %q", m.form.err)
	}
	_ = b
}

func TestDockedLayoutUsesFullWidth(t *testing.T) {
	m, _ := newSurface(t)
	m.Resize(200, 50)
	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) < 45 {
		t.Fatalf("docked view should fill height, got %d lines", len(lines))
	}
	maxW := 0
	for _, l := range lines {
		if w := len([]rune(ansi.Strip(l))); w > maxW {
			maxW = w
		}
	}
	if maxW < 170 {
		t.Fatalf("docked layout wastes width: widest line %d < 170", maxW)
	}
	// Rail lists every section with counts.
	plain := ansi.Strip(out)
	for _, want := range []string{"INBOX", "BOARD", "CHANNELS", "REPOS"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rail missing %q", want)
		}
	}
}

func TestModalOverlayKeepsRailVisible(t *testing.T) {
	m, _ := newSurface(t)
	m.Resize(200, 50)
	m.sec = secRepos
	m.HandleKey("a") // add-member modal
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "REPOS") || !strings.Contains(out, "INBOX") {
		t.Fatal("rail hidden while modal open")
	}
	if !strings.Contains(out, "add member") {
		t.Fatalf("modal missing:\n%s", out[:400])
	}
}

var _ = time.Now
