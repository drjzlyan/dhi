package tasks

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestCommentsPersistAndRecordActivity(t *testing.T) {
	s, ws := setupStore(t)
	if err := s.Create("c", "Card", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AddComment("c", "alice", "first line\nsecond"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatusAs("c", Active, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus("c", Active); err != nil { // no-op move records nothing
		t.Fatal(err)
	}
	if err := s.AssignAs("c", "bob", "you"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPriority("c", "high"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabels("c", []string{"Bug", "ui"}); err != nil {
		t.Fatal(err)
	}

	re, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := re.Get("c")
	if len(got.Comments) != 1 || got.Comments[0].Author != "alice" {
		t.Fatalf("comments = %+v", got.Comments)
	}
	var kinds []string
	for _, a := range got.Activity {
		kinds = append(kinds, a.Kind+":"+a.From+">"+a.To+"@"+a.Actor)
	}
	want := []string{
		"comment:>first line@alice",
		"status:backlog>active@alice",
		"assignee:>bob@you",
		"priority:>high@",
		"labels:>bug,ui@",
	}
	if strings.Join(kinds, "|") != strings.Join(want, "|") {
		t.Fatalf("activity = %v\nwant     %v", kinds, want)
	}
}

func TestCommentRefusals(t *testing.T) {
	s, _ := setupStore(t)
	_ = s.Create("c", "Card", "", "")
	for name, fn := range map[string]func() error{
		"empty":        func() error { return s.AddComment("c", "alice", "  ") },
		"bad author":   func() error { return s.AddComment("c", "Bad Author!", "x") },
		"too long":     func() error { return s.AddComment("c", "alice", strings.Repeat("x", maxCommentLen+1)) },
		"unknown task": func() error { return s.AddComment("nope", "alice", "x") },
	} {
		if fn() == nil {
			t.Errorf("%s: want refusal", name)
		}
	}
}

func TestActivityIsCapped(t *testing.T) {
	s, _ := setupStore(t)
	_ = s.Create("c", "Card", "", "")
	for i := 0; i < maxActivity+20; i++ {
		if err := s.AddComment("c", "alice", fmt.Sprintf("n%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Get("c")
	if len(got.Activity) != maxActivity {
		t.Fatalf("activity len = %d, want %d", len(got.Activity), maxActivity)
	}
	if got.Activity[len(got.Activity)-1].To != fmt.Sprintf("n%d", maxActivity+19) {
		t.Fatal("newest entry must survive trimming")
	}
	if len(got.Comments) != maxActivity+20 {
		t.Fatalf("comments must not be trimmed, got %d", len(got.Comments))
	}
}

func TestConcurrentCommentsDoNotLoseWrites(t *testing.T) {
	s, ws := setupStore(t)
	_ = s.Create("c", "Card", "", "")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.AddComment("c", "alice", fmt.Sprintf("m%d", i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	re, _ := Open(ws)
	got, _ := re.Get("c")
	if len(got.Comments) != 12 {
		t.Fatalf("persisted comments = %d, want 12", len(got.Comments))
	}
}

func TestSchema2CardLoadsAndUpgradesOnWrite(t *testing.T) {
	s, ws := setupStore(t)
	_ = s.Create("old", "Old card", "", "")
	path := s.cardPath("old")
	data, _ := os.ReadFile(path)
	legacy := strings.Replace(string(data), "schema = 3", "schema = 2", 1)
	if legacy == string(data) {
		t.Fatal("fixture card is not schema 3")
	}
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	re, err := Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := re.Get("old"); !ok || len(got.Comments) != 0 {
		t.Fatalf("schema-2 card = %+v ok=%v warns=%v", got, ok, re.Warnings())
	}
	if err := re.AddComment("old", "alice", "hi"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "schema = 3") {
		t.Fatal("write must upgrade the card to schema 3")
	}
}
