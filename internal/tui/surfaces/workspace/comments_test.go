package workspace

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/drjzlyan/dhi/internal/tasks"
)

func TestBoardCommentFlowAndDetail(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard
	m.Resize(150, 40) // wide: the side pane shows comments + activity
	_ = store.Create("login", "Fix login", "", "")
	_ = store.AddComment("login", "alice", "root cause is the cache")
	_ = store.SetStatusAs("login", tasks.Active, "alice")

	if !m.HandleKey("l") { // select the active lane's card
		t.Fatal("lane key not consumed")
	}
	if !m.HandleKey("N") || m.form.kind != fTaskComment {
		t.Fatalf("N must open the comment form, kind=%v", m.form.kind)
	}
	for _, r := range "ship it" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	tk, _ := store.Get("login")
	if len(tk.Comments) != 2 || tk.Comments[1].Author != "you" || tk.Comments[1].Text != "ship it" {
		t.Fatalf("comments = %+v (form err %q)", tk.Comments, m.form.err)
	}

	out := ansi.Strip(m.View())
	for _, want := range []string{"COMMENTS (2)", "root cause is the cache", "ship it", "ACTIVITY", "alice moved backlog → active"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q\n%s", want, out)
		}
	}
}

func TestBoardNotesNarrowIsOneLine(t *testing.T) {
	tk := tasks.Task{Slug: "a", Title: "A", Comments: []tasks.Comment{{Author: "bo", Text: "line one\nline two"}}}
	got := boardNotesLines(tk, 40, false, nil)
	if len(got) != 1 || !strings.Contains(ansi.Strip(got[0]), "1 comments · bo: line one") {
		t.Fatalf("narrow notes = %q", got)
	}
	if boardNotesLines(tasks.Task{}, 40, true, nil) != nil {
		t.Fatal("no notes → no lines")
	}
}
