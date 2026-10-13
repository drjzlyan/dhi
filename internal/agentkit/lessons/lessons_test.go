package lessons

import (
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	s := Open(t.TempDir())
	s.now = func() time.Time { return now }
	return s, &now
}

func TestAddDedupesAndRaisesConfidence(t *testing.T) {
	s, _ := testStore(t)
	a, added, err := s.Add(Lesson{Agent: "dev", Text: "Run go test with -race before committing.", Confidence: 0.5, Runs: []string{"r1"}, Source: SourceReflection})
	if err != nil || !added || a.Status != StatusActive || !strings.HasPrefix(a.ID, "l-") {
		t.Fatalf("first add = %+v %v %v", a, added, err)
	}
	b, added, err := s.Add(Lesson{Agent: "dev", Text: "run go test with -race before committing", Confidence: 0.5, Runs: []string{"r2"}})
	if err != nil || added || b.ID != a.ID || b.Confidence <= a.Confidence || len(b.Runs) != 2 {
		t.Fatalf("duplicate must merge: %+v added=%v %v", b, added, err)
	}
	if _, _, err := s.Add(Lesson{Agent: "dev", Text: ""}); err == nil {
		t.Fatal("empty lesson must refuse")
	}
	if _, _, err := s.Add(Lesson{Agent: "../x", Text: "y"}); err == nil {
		t.Fatal("bad agent id must refuse")
	}
}

func TestRetrieveRanksAndPins(t *testing.T) {
	s, _ := testStore(t)
	mustAdd := func(l Lesson) Lesson {
		t.Helper()
		out, _, err := s.Add(l)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	race := mustAdd(Lesson{Agent: "dev", Text: "Run go test with -race in this repo", Tags: []string{"go", "tests"}, Confidence: 0.6})
	mustAdd(Lesson{Agent: "dev", Text: "Keep CSS changes scoped to one component", Tags: []string{"css"}, Confidence: 0.6})
	pin := mustAdd(Lesson{Scope: ScopeUser, Text: "The human prefers small pull requests", Confidence: 0.9})
	if err := s.Set(ScopeUser, "", pin.ID, func(l *Lesson) { l.Status = StatusPinned }); err != nil {
		t.Fatal(err)
	}
	got, err := s.Retrieve("dev", "fix the failing go tests", 1)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Retrieve("dev", "fix the failing go tests", 1); len(again) != 2 {
		t.Fatal("retrieval must be repeatable")
	}
	s.MarkUsed(got)
	if len(got) != 2 || got[0].ID != pin.ID || got[1].ID != race.ID {
		t.Fatalf("retrieve = %+v", got)
	}
	all, _ := s.List("dev")
	for _, l := range all {
		if (l.ID == race.ID || l.ID == pin.ID) && l.Uses != 1 {
			t.Fatalf("retrieved lessons must be marked used: %+v", l)
		}
	}
	if other, _ := s.Retrieve("qa", "anything", 5); len(other) != 1 || other[0].ID != pin.ID {
		t.Fatalf("user lessons apply to every agent; private ones do not: %+v", other)
	}
}

func TestRetiredNeverInjected(t *testing.T) {
	s, _ := testStore(t)
	l, _, _ := s.Add(Lesson{Agent: "dev", Text: "Use tabs in Makefiles", Confidence: 0.9})
	if err := s.Set(ScopePrivate, "dev", l.ID, func(x *Lesson) { x.Status = StatusRetired }); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Retrieve("dev", "Makefile tabs", 5); len(got) != 0 {
		t.Fatalf("retired lesson injected: %+v", got)
	}
	// Learned again: it comes back.
	back, added, _ := s.Add(Lesson{Agent: "dev", Text: "use tabs in makefiles"})
	if added || back.Status != StatusActive {
		t.Fatalf("relearning must reactivate: %+v", back)
	}
}

func TestConsolidateDecays(t *testing.T) {
	s, now := testStore(t)
	weak, _, _ := s.Add(Lesson{Agent: "dev", Text: "Maybe prefer table tests", Confidence: 0.3})
	strong, _, _ := s.Add(Lesson{Agent: "dev", Text: "Never force-push shared branches", Confidence: 0.9})
	*now = now.Add(45 * 24 * time.Hour)
	rep, err := s.Consolidate("dev", 30)
	if err != nil || rep.Retired != 1 {
		t.Fatalf("report = %+v %v", rep, err)
	}
	all, _ := s.List("dev")
	for _, l := range all {
		if l.ID == weak.ID && l.Status != StatusRetired || l.ID == strong.ID && l.Status != StatusActive {
			t.Fatalf("decay wrong: %+v", l)
		}
	}
}

// F-078: the lifecycle keeps a lesson file bounded — stale lessons
// retire (established ones later), the weakest past the cap retire,
// long-retired ones move to the archive, and pinned ones ride capped.
func TestConsolidateWithPolicy(t *testing.T) {
	s, now := testStore(t)
	add := func(text string, conf float64, idleDays int) {
		t.Helper()
		l, _, err := s.Add(Lesson{Agent: "dev", Text: text, Confidence: conf, Source: SourceAgent})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.Set(ScopePrivate, "dev", l.ID, func(x *Lesson) { x.LastUsed = now.Add(-time.Duration(idleDays) * 24 * time.Hour) })
	}
	add("prefer table tests in Go packages", 0.6, 40)
	add("run the linter before every commit", 0.9, 40)
	add("keep pull requests under four hundred lines", 0.9, 70)
	add("document exported functions with examples", 0.6, 1)
	rep, err := s.ConsolidateWith("dev", Policy{StaleDays: 30, ActiveMax: 1, PurgeDays: 90})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	all, _ := s.List("dev")
	for _, l := range all {
		status[l.Text] = l.Status
	}
	if status["prefer table tests in Go packages"] != StatusRetired || status["keep pull requests under four hundred lines"] != StatusRetired {
		t.Fatalf("stale not retired: %v", status)
	}
	// Two remain active before the cap of 1: the weaker of them retires.
	active := 0
	for _, st := range status {
		if st == StatusActive {
			active++
		}
	}
	if active != 1 || rep.Retired != 3 {
		t.Fatalf("active=%d report=%+v statuses=%v", active, rep, status)
	}
	*now = now.Add(100 * 24 * time.Hour)
	rep, _ = s.ConsolidateWith("dev", Policy{StaleDays: 30, ActiveMax: 10, PurgeDays: 90})
	if rep.Purged != 3 {
		t.Fatalf("purged = %d", rep.Purged)
	}

	for _, txt := range []string{"always rebase before merging", "never commit generated files", "name migrations by date"} {
		l, _, _ := s.Add(Lesson{Agent: "dev", Text: txt, Confidence: 0.9})
		_ = s.Set(ScopePrivate, "dev", l.ID, func(x *Lesson) { x.Status = StatusPinned })
	}
	s.SetPinnedMax(2)
	got, _ := s.Retrieve("dev", "anything", 0)
	pinned := 0
	for _, l := range got {
		if l.Status == StatusPinned {
			pinned++
		}
	}
	if pinned != 2 || s.PinnedCount("dev") != 3 {
		t.Fatalf("pinned in prompt = %d, stored %d", pinned, s.PinnedCount("dev"))
	}
}
