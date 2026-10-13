package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// boardFixture is a workspace surface with a task store whose attach seam
// makes plain directories, plus a bus for hand-offs.
func boardFixture(t *testing.T) (*Model, *workspace.Workspace, *tasks.Store, *bus.Bus) {
	t.Helper()
	m, ws, b := newSurfaceWithBus(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	store.SetAttach(func(slug, member, branch, sp string) (string, error) {
		rel := ".dhi/tasks/" + slug + "/" + member
		return rel, os.MkdirAll(filepath.Join(ws.Root, rel), 0o755)
	}, func(string, string) error { return nil })
	m.taskStore = store
	m.sec = secBoard
	return m, ws, store, b
}

func TestBoardCardOwnerChips(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	cases := []struct {
		tk      tasks.Task
		working bool
		want    string
	}{
		{tasks.Task{Slug: "a", Title: "A", Assignee: "forge"}, true, "● forge"},
		{tasks.Task{Slug: "b", Title: "B", Assignee: "forge"}, false, "○ forge"},
		{tasks.Task{Slug: "c", Title: "C", Assignee: bus.Human}, false, "you"},
		{tasks.Task{Slug: "d", Title: "D"}, false, "unassigned"},
		{tasks.Task{Slug: "e", Title: "E", Assignee: "sage", TestsPass: true, PRNumber: 12,
			Comments: []tasks.Comment{{Author: "you", Text: "hi"}}}, false, "○ sage  ✓ #12 ↳1  e"},
	}
	var all []string
	for _, c := range cases {
		card := ansi.Strip(boardCard(c.tk, 40, false, c.working))
		if meta := strings.Split(card, "\n")[1]; !strings.HasPrefix(strings.TrimSpace(meta), c.want) {
			t.Errorf("%s: meta = %q, want prefix %q", c.tk.Slug, meta, c.want)
		}
		all = append(all, card)
	}
	all = append(all, ansi.Strip(boardCard(cases[0].tk, 16, false, true))) // narrow lane
	golden.Snapshot(t, "board_cards_f073", strings.Join(all, "\n---\n"))
}

func TestCardPageShowsFormattedComments(t *testing.T) {
	m, _, store, _ := boardFixture(t)
	if err := store.Create("rate-limit", "Rate-limit the public API", "forge", ""); err != nil {
		t.Fatal(err)
	}
	_ = store.AddComment("rate-limit", "forge", "Bucket per client:\n\n```go\nb.tokens--\n```\n\n- default `100/min`")
	_ = store.AddComment("rate-limit", bus.Human, "ship it")
	m.HandleKey("enter")
	if m.card == nil {
		t.Fatal("enter did not open the card page")
	}
	view := m.View()
	plain := ansi.Strip(view)
	for _, want := range []string{"Rate-limit the public API", "ASSIGNEE", "○ forge", "WORK", "COMMENTS (2)",
		"b.tokens--", "• default 100/min", "ship it", "esc back"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("card page missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "```") {
		t.Fatalf("raw fence in comments:\n%s", plain)
	}
	for _, tk := range []theme.Tokens{theme.Dark(), theme.Light(), theme.HighContrast()} {
		theme.SwapForTest(t, tk)
		golden.AssertBgIntegrity(t, "card page "+tk.Name, m.View())
	}
	m.HandleKey("j")
	m.HandleKey("esc")
	if m.card != nil {
		t.Fatal("esc did not close the card page")
	}
}

// cancelTurner records hand-offs and cancellations.
type cancelTurner struct {
	handled   []bus.Message
	cancelled []string
}

func (c *cancelTurner) Handle(_ context.Context, m bus.Message) { c.handled = append(c.handled, m) }
func (c *cancelTurner) CancelThread(ch string, th int64) int {
	c.cancelled = append(c.cancelled, threadRef(ch, th))
	return 1
}

func TestReassignStopsBusyAgentAndHandsOffWorktree(t *testing.T) {
	m, ws, store, b := boardFixture(t)
	rt := &cancelTurner{}
	m.rt = rt
	rosterAgent(t, ws, "forge", "fixer")
	rosterAgent(t, ws, "sage", "reviewer")
	if err := store.Create("login", "Fix login timeout", "forge", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.BindThread("login", "dm:forge", 3); err != nil {
		t.Fatal(err)
	}
	if err := store.Attach("login", "alpha", "task/login", ""); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("a")
	if m.assign == nil {
		t.Fatal("a did not open the picker")
	}
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "assign login") || !strings.Contains(plain, "sage") {
		t.Fatalf("picker not rendered:\n%s", plain)
	}
	golden.AssertBgIntegrity(t, "assign picker", m.View())
	for _, k := range []string{"s", "a", "g", "e"} {
		m.HandleKey(k)
	}
	m.HandleKey("enter")
	tk, _ := store.Get("login")
	if tk.Assignee != "sage" {
		t.Fatalf("assignee = %q (flash %q)", tk.Assignee, m.form.err)
	}
	if len(rt.cancelled) != 1 || rt.cancelled[0] != "dm:forge#3" {
		t.Fatalf("cancelled = %v", rt.cancelled)
	}
	if len(rt.handled) != 1 {
		t.Fatalf("hand-offs = %d", len(rt.handled))
	}
	brief := rt.handled[0].Text
	for _, want := range []string{"picking this up from forge", ".dhi/tasks/login/alpha", "task/login"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief %q missing %q", brief, want)
		}
	}
	// The brief goes to the new owner's DM, never the old owner's thread,
	// and the card follows it there.
	if ch := rt.handled[0].Channel; ch != "dm:sage" {
		t.Fatalf("brief posted to %s", ch)
	}
	if tk, _ := store.Get("login"); tk.ThreadChannel != "dm:sage" {
		t.Fatalf("card thread = %s", tk.ThreadChannel)
	}
	_ = b
	if c := tk.Comments; len(c) == 0 || !strings.Contains(c[len(c)-1].Text, "Stopped forge's turn") {
		t.Fatalf("no comment recording the stop: %+v", c)
	}
}

func TestTakeCardAssignsProvisionsAndOpensEditor(t *testing.T) {
	m, ws, store, _ := boardFixture(t)
	var opened []string
	m.openWorktree = func(label, dir string) bool { opened = append(opened, label+"="+dir); return true }
	m.branchForFn = func(slug string) string { return "feature/" + slug }
	if err := store.Create("csv", "CSV export", "", ""); err != nil {
		t.Fatal(err)
	}
	m.HandleKey("T")
	// Two members: the attach form asks which, branch prefilled.
	if m.form.kind != fTaskAttach || m.form.values()[1] != "feature/csv" {
		t.Fatalf("form = %v values=%v", m.form.kind, m.form.values())
	}
	typeInto(t, m, 0, "beta")
	m.HandleKey("enter")
	tk, _ := store.Get("csv")
	if tk.Assignee != bus.Human || tk.Status != tasks.Active {
		t.Fatalf("after take: assignee=%q status=%s", tk.Assignee, tk.Status)
	}
	if len(tk.ChangeSets) != 1 || tk.ChangeSets[0].Branch != "feature/csv" {
		t.Fatalf("changesets = %+v", tk.ChangeSets)
	}
	want := "csv (feature/csv)=" + filepath.Join(ws.Root, ".dhi/tasks/csv/beta")
	if len(opened) != 1 || opened[0] != want {
		t.Fatalf("opened = %v, want %s", opened, want)
	}
	// Taking a card that already has a worktree just opens it.
	m.HandleKey("T")
	if len(opened) != 2 {
		t.Fatalf("second take opened %v", opened)
	}
}

func TestCardCommentAgeAndOrder(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	lines := commentLines(tasks.Comment{Author: "forge", Text: "done", At: now.Add(-2 * time.Hour)}, 40, now)
	if got := ansi.Strip(lines[0]); !strings.Contains(got, "forge") || !strings.Contains(got, "2h") {
		t.Fatalf("header = %q", got)
	}
}

// rosterAgent writes a loadable agent manifest (schema 6).
func rosterAgent(t *testing.T, ws *workspace.Workspace, id, role string) {
	t.Helper()
	dir := filepath.Join(ws.Root, ".dhi", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "schema = 6\nname = \"" + id + "\"\nmodel = \"default\"\nrole = \"" + role + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, id+".toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}
