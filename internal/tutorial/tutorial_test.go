package tutorial

import (
	"strings"
	"testing"
)

func TestEveryLessonFileParses(t *testing.T) {
	entries, _ := lessonsFS.ReadDir("lessons")
	if got := len(All()); got != len(entries) || got < 4 {
		t.Fatalf("All() = %d of %d lesson files — one fails to parse", got, len(entries))
	}
	var slugs []string
	for _, tu := range All() {
		slugs = append(slugs, tu.Slug)
		for i, s := range tu.Steps {
			if !ValidAwait(s.Await) {
				t.Errorf("%s step %d: bad await %q", tu.Slug, i+1, s.Await)
			}
			if len(s.Body) > 260 {
				t.Errorf("%s step %d: body is %d chars — the coach strip shows two lines", tu.Slug, i+1, len(s.Body))
			}
		}
	}
	if strings.Join(slugs, ",") != "tour,team,editor,review,debug" {
		t.Fatalf("teaching order = %v", slugs)
	}
}

func TestTheTourTeachesWithObservableSteps(t *testing.T) {
	tour, ok := Get("tour")
	if !ok {
		t.Fatal("no tour")
	}
	awaits := map[string]bool{}
	for _, s := range tour.Steps {
		awaits[s.Await] = true
	}
	for _, want := range []string{"view:editor", "view:workspace", "palette", "help"} {
		if !awaits[want] {
			t.Errorf("the tour never waits for %q", want)
		}
	}
	if last := tour.Steps[len(tour.Steps)-1]; last.Await != "" {
		t.Errorf("the last step must be a manual finish, awaits %q", last.Await)
	}
}

func TestAwaitGrammar(t *testing.T) {
	for in, want := range map[string]bool{
		"": true, "palette": true, "help": true, "view:editor": true, "view:settings": true,
		"view:nope": false, "view:": false, "key:x": false, "Palette": false,
		"do:editor.save": true, "do:task.created": true, "do:nope": false, "do:": false, "do": false,
	} {
		if ValidAwait(in) != want {
			t.Errorf("ValidAwait(%q) = %v, want %v", in, !want, want)
		}
	}
}

func TestParseRejectsBrokenLessons(t *testing.T) {
	ok := "schema = 1\nname = \"n\"\ndescription = \"d\"\n[[step]]\ntitle = \"t\"\nbody = \"b\"\n"
	if _, err := parse("x", []byte(ok)); err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"unknown key": ok + "mood = \"x\"\n",
		"bad await":   ok + "await = \"view:mars\"\n",
		"no steps":    "schema = 1\nname = \"n\"\ndescription = \"d\"\n",
		"empty body":  "schema = 1\nname = \"n\"\ndescription = \"d\"\n[[step]]\ntitle = \"t\"\nbody = \"\"\n",
		"no name":     "schema = 1\ndescription = \"d\"\n[[step]]\ntitle = \"t\"\nbody = \"b\"\n",
		"bad schema":  strings.Replace(ok, "schema = 1", "schema = 2", 1),
	} {
		if _, err := parse("x", []byte(doc)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestEveryActionEventHasADescriptionAndLessonsOnlyWaitForRealOnes(t *testing.T) {
	for _, name := range EventNames() {
		if strings.TrimSpace(Events[name]) == "" {
			t.Errorf("event %q has no description", name)
		}
	}
	used := map[string]bool{}
	for _, tu := range All() {
		for _, s := range tu.Steps {
			if ev, ok := strings.CutPrefix(s.Await, "do:"); ok {
				used[ev] = true
			}
		}
	}
	for _, ev := range []string{EvEditorOpen, EvEditorInsert, EvEditorSave, EvEditorPair, EvEditorTest,
		EvEditorBreak, EvEditorDebug, EvTaskCreated, EvReviewOpened, EvReviewComment, EvReviewSubmitted} {
		if !used[ev] {
			t.Errorf("no lesson waits for %q — a dead event", ev)
		}
	}
}
