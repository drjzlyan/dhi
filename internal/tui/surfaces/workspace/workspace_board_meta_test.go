package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
)

func boardMetaSurface(t *testing.T) (*Model, *tasks.Store) {
	t.Helper()
	m, ws := newSurface(t)
	st, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = st
	m.sec = secBoard
	return m, st
}

func TestBoardMetadataForms(t *testing.T) {
	m, st := boardMetaSurface(t)
	if err := st.Create("card", "Card", "", ""); err != nil {
		t.Fatal(err)
	}
	m.boardActive, m.boardCur[0] = 0, 0

	// Labels.
	m.HandleKey("L")
	if m.form.kind != fTaskLabels {
		t.Fatalf("L opened %v", m.form.kind)
	}
	typeInto(t, m, 0, "ui, bug")
	m.HandleKey("enter")
	tk, _ := st.Get("card")
	if strings.Join(tk.Labels, ",") != "bug,ui" {
		t.Fatalf("labels = %v", tk.Labels)
	}
	// Priority (toggle field): right-cycles unset → low → normal → high.
	m.HandleKey("P")
	for i := 0; i < 3; i++ {
		m.HandleKey("right")
	}
	m.HandleKey("enter")
	tk, _ = st.Get("card")
	if tk.Priority != tasks.PriorityHigh {
		t.Fatalf("priority = %q", tk.Priority)
	}
	// Epic.
	m.HandleKey("E")
	typeInto(t, m, 0, "checkout")
	m.HandleKey("enter")
	// Due.
	m.HandleKey("D")
	typeInto(t, m, 0, "2026-10-01")
	m.HandleKey("enter")
	tk, _ = st.Get("card")
	if tk.Epic != "checkout" || tk.Due != "2026-10-01" {
		t.Fatalf("meta = %+v", tk)
	}
	// The detail pane shows the metadata.
	out := ansi.Strip(m.boardBody(110, 24))
	for _, want := range []string{"labels bug,ui", "priority high", "epic checkout", "due 2026-10-01"} {
		if !strings.Contains(out, want) {
			t.Fatalf("board missing %q:\n%s", want, out)
		}
	}
}

func TestBoardFilterCrossCardSearch(t *testing.T) {
	m, st := boardMetaSurface(t)
	_ = st.Create("login", "Fix login", "", "")
	_ = st.Create("billing", "Billing retries", "", "")
	if err := st.SetLabels("billing", []string{"payments"}); err != nil {
		t.Fatal(err)
	}
	_ = st.SetEpic("login", "auth")

	// Filter by label.
	m.boardFilter = "payments"
	g := m.boardGroups()
	if n := len(g[0]); n != 1 || g[0][0].Slug != "billing" {
		t.Fatalf("label filter = %+v", g[0])
	}
	// Filter by epic.
	m.boardFilter = "auth"
	g = m.boardGroups()
	if n := len(g[0]); n != 1 || g[0][0].Slug != "login" {
		t.Fatalf("epic filter = %+v", g[0])
	}
	// Filter by title substring.
	m.boardFilter = "retries"
	g = m.boardGroups()
	if n := len(g[0]); n != 1 || g[0][0].Slug != "billing" {
		t.Fatalf("title filter = %+v", g[0])
	}
	// `/` edits: type narrows live, enter keeps, esc clears.
	m.boardFilter = ""
	m.HandleKey("/")
	if !m.boardFilterEdit {
		t.Fatal("/ did not enter filter edit")
	}
	for _, r := range "login" {
		m.HandleKey(string(r))
	}
	if m.boardFilter != "login" {
		t.Fatalf("typed filter = %q", m.boardFilter)
	}
	m.HandleKey("enter")
	if m.boardFilterEdit || m.boardFilter != "login" {
		t.Fatalf("enter: edit=%v filter=%q", m.boardFilterEdit, m.boardFilter)
	}
	m.HandleKey("/")
	m.HandleKey("esc")
	if m.boardFilter != "" || m.boardFilterEdit {
		t.Fatalf("esc did not clear: %q", m.boardFilter)
	}
}

func TestBoardBulkMove(t *testing.T) {
	m, st := boardMetaSurface(t)
	_ = st.Create("a", "A", "", "")
	_ = st.Create("b", "B", "", "")
	m.boardActive, m.boardCur[0] = 0, 0

	m.HandleKey(" ")
	m.HandleKey("j")
	m.HandleKey(" ")
	if len(m.boardMarks) != 2 {
		t.Fatalf("marks = %v", m.boardMarks)
	}
	if !strings.Contains(ansi.Strip(m.boardBody(110, 24)), "2 marked") {
		t.Fatal("marks line missing")
	}
	m.HandleKey("M")
	if m.form.kind != fTaskBulkMove {
		t.Fatalf("M opened %v", m.form.kind)
	}
	m.HandleKey("right") // backlog → active
	m.HandleKey("enter")
	for _, slug := range []string{"a", "b"} {
		tk, _ := st.Get(slug)
		if tk.Status != tasks.Active {
			t.Fatalf("%s status = %v", slug, tk.Status)
		}
	}
	if len(m.boardMarks) != 0 {
		t.Fatalf("marks not cleared: %v", m.boardMarks)
	}
	// M with nothing marked refuses with a hint, no form.
	m.HandleKey("M")
	if m.form.kind == fTaskBulkMove {
		t.Fatal("M opened bulk move with no marks")
	}
}

func TestBoardCardPriorityPrefix(t *testing.T) {
	out := ansi.Strip(boardCard(tasks.Task{Slug: "x", Title: "X", Priority: tasks.PriorityUrgent}, 40, true, false))
	if !strings.Contains(out, "◆") || !strings.Contains(out, "▲") {
		t.Fatalf("card prefix = %q", out)
	}
	if !strings.Contains(ansi.Strip(boardCard(tasks.Task{Slug: "x", Title: "X"}, 40, false, false)), "x") {
		t.Fatal("plain card missing slug")
	}
}
