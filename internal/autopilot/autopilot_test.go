package autopilot

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		in      string
		wantOK  bool
		wantErr string // substring; empty = OK
	}{
		{"interval 30m", true, ""},
		{"interval 1h30m", true, ""},
		{"interval 10s", true, ""},
		{"daily 09:00", true, ""},
		{"daily 00:00", true, ""},
		{"weekly mon 08:30", true, ""},
		{"weekly sun 23:59", true, ""},
		{"weekly tue 00:00", true, ""},
		{"daily 25:00", false, `bad time "25:00"`},
		{"daily 09:61", false, `bad time "09:61"`},
		{"weekly xmas 08:30", false, `unknown day "xmas"`},
		{"weekly mon 8", false, `bad time "8"`},
		{"30m", false, `schedule "30m"`},
		{"", false, "empty schedule"},
		{"interval", false, `want "interval <dur>"`},
		{"interval -5m", false, `bad interval "-5m"`},
		{"daily", false, `want "daily HH:MM"`},
		{"hourly 5", false, `want "interval <dur>", "daily HH:MM", or "weekly <dow> HH:MM"`},
	}
	for _, tt := range tests {
		sch, err := ParseSchedule(tt.in)
		if tt.wantOK {
			if err != nil {
				t.Errorf("ParseSchedule(%q) unexpected error: %v", tt.in, err)
			}
			if sch.String() != tt.in {
				t.Errorf("ParseSchedule(%q) round-trip: %q", tt.in, sch.String())
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseSchedule(%q) should fail", tt.in)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("ParseSchedule(%q) = %q, want containing %q", tt.in, err, tt.wantErr)
		}
	}
}

func clock(y, m, d, hh, mm int) time.Time {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC)
}

func card(sch string, last time.Time) Card {
	s, err := ParseSchedule(sch)
	if err != nil {
		panic(err)
	}
	return Card{Slug: "c", Name: "c", Agent: "a", Prompt: "p", Schedule: s, Enabled: true, LastRun: last}
}

func TestCardDue(t *testing.T) {
	tests := []struct {
		name string
		c    Card
		now  time.Time
		want bool
	}{
		{"interval first run never ran", card("interval 30m", time.Time{}), clock(2026, 9, 2, 8, 0), true},
		{"interval elapsed", card("interval 30m", clock(2026, 9, 2, 8, 0)), clock(2026, 9, 2, 8, 31), true},
		{"interval boundary exact", card("interval 30m", clock(2026, 9, 2, 8, 0)), clock(2026, 9, 2, 8, 30), true},
		{"interval not elapsed", card("interval 30m", clock(2026, 9, 2, 8, 0)), clock(2026, 9, 2, 8, 29), false},
		{"interval multi elapsed = one catch-up", card("interval 30m", clock(2026, 9, 2, 8, 0)), clock(2026, 9, 2, 11, 0), true},
		{"daily before instant", card("daily 17:00", time.Time{}), clock(2026, 9, 2, 16, 59), false},
		{"daily at instant never ran", card("daily 17:00", time.Time{}), clock(2026, 9, 2, 17, 0), true},
		{"daily past instant never ran", card("daily 17:00", time.Time{}), clock(2026, 9, 2, 17, 1), true},
		{"daily ran today before instant", card("daily 17:00", clock(2026, 9, 2, 9, 0)), clock(2026, 9, 2, 18, 0), true},
		{"daily ran after instant not due", card("daily 17:00", clock(2026, 9, 2, 17, 0)), clock(2026, 9, 2, 17, 5), false},
		{"daily across midnight same day new instant", card("daily 09:00", clock(2026, 9, 2, 9, 0)), clock(2026, 9, 3, 9, 0), true},
		{"daily not yet next day", card("daily 09:00", clock(2026, 9, 2, 9, 0)), clock(2026, 9, 3, 8, 59), false},
		{"weekly mon before instant", card("weekly mon 08:30", time.Time{}), clock(2026, 9, 7, 8, 29), false},
		{"weekly mon at instant", card("weekly mon 08:30", time.Time{}), clock(2026, 9, 7, 8, 30), true},
		{"weekly ran tue after mon instant not due", card("weekly mon 08:30", clock(2026, 9, 8, 12, 0)), clock(2026, 9, 9, 8, 30), false},
		{"weekly ran prior week mon due again", card("weekly mon 08:30", clock(2026, 8, 31, 8, 30)), clock(2026, 9, 7, 9, 0), true},
		{"weekly dow wrap single catch-up after many weeks", card("weekly mon 08:30", clock(2026, 8, 31, 8, 30)), clock(2026, 9, 21, 9, 0), true},
		{"weekly sun same week", card("weekly sun 23:00", clock(2026, 9, 6, 23, 0)), clock(2026, 9, 8, 0, 0), false},
	}
	for _, tt := range tests {
		if got := tt.c.Due(tt.now); got != tt.want {
			t.Errorf("%s: due(%v) = %v, want %v", tt.name, tt.now, got, tt.want)
		}
	}
}

func TestDueSkipsPaused(t *testing.T) {
	s := withStore(t, map[string]string{"a": `schema = 1
name = "A"
agent = "a"
prompt = "p"
schedule = "interval 1m"
enabled = true
`})
	if err := s.MarkRan("a", clock(2026, 9, 2, 8, 0)); err != nil {
		t.Fatal(err)
	}
	now := clock(2026, 9, 2, 8, 5)
	if len(s.Due(now)) != 1 {
		t.Fatalf("enabled due card = %d, want 1", len(s.Due(now)))
	}
	if err := s.SetEnabled("a", false); err != nil {
		t.Fatal(err)
	}
	if len(s.Due(now)) != 0 {
		t.Error("paused card must never be due")
	}
}
func withStore(t *testing.T, cards map[string]string) *Store {
	t.Helper()
	ws := &workspace.Workspace{Root: t.TempDir()}
	s, err := Open(ws)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for slug, body := range cards {
		if err := seedCard(s, slug, body); err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
	}
	// Re-open so seeded cards are actually loaded.
	s, err = Open(ws)
	if err != nil {
		t.Fatalf("Open after seed: %v", err)
	}
	return s
}

func seedCard(s *Store, slug, body string) error {
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.cardPath(slug), []byte(strings.TrimSpace(body)), 0o644)
}

func TestOpenAbsentDirGivesEmptyStore(t *testing.T) {
	ws := &workspace.Workspace{Root: t.TempDir()}
	s, err := Open(ws)
	if err != nil {
		t.Fatalf("Open on empty root: %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatalf("want empty store, got %d cards", len(s.List()))
	}
	if len(s.Warnings()) != 0 {
		t.Fatalf("want no warnings, got %v", s.Warnings())
	}
}

func TestStrictDecodeRefusesUnknownKey(t *testing.T) {
	s := withStore(t, map[string]string{"standup": `schema = 1
name = "Standup"
agent = "scout"
prompt = "summarize"
schedule = "daily 09:00"
enabled = true
bogus = 1
`})
	if len(s.List()) != 0 {
		t.Fatalf("unknown-key card must be skipped")
	}
	w := s.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "unknown key(s): bogus") {
		t.Fatalf("warnings = %v, want unknown-key refusal", w)
	}
}

func TestMalformedCardWarns(t *testing.T) {
	s := withStore(t, map[string]string{"standup": `schema = 1
name = ""
agent = "scout"
prompt = "p"
schedule = "daily 09:00"
`})
	if len(s.List()) != 0 {
		t.Fatalf("empty-name card must be skipped")
	}
	w := s.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "name required") {
		t.Fatalf("warnings = %v, want name-required refusal", w)
	}
}

func TestMarkRanRoundTripsThroughToml(t *testing.T) {
	s := withStore(t, map[string]string{"standup": `schema = 1
name = "Standup"
agent = "scout"
prompt = "summarize"
schedule = "daily 09:00"
enabled = true
`})
	if err := s.MarkRan("standup", clock(2026, 9, 2, 9, 0)); err != nil {
		t.Fatalf("MarkRan: %v", err)
	}
	re, err := Open(s.ws)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	c, ok := re.Get("standup")
	if !ok {
		t.Fatal("card lost on re-open")
	}
	if !c.LastRun.Equal(clock(2026, 9, 2, 9, 0)) {
		t.Fatalf("LastRun = %v, want %v", c.LastRun, clock(2026, 9, 2, 9, 0))
	}
	if c.Schedule.String() != "daily 09:00" {
		t.Fatalf("schedule text lost: %q", c.Schedule.String())
	}
}

func TestCreatePersistenceAndSubscribe(t *testing.T) {
	s := withStore(t, nil)
	ch, cancel := s.Subscribe()
	defer cancel()
	c, err := s.Create("standup", "Standup", "scout", "summarize", mustSch(t, "daily 09:00"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !c.Enabled {
		t.Error("new cards default to enabled")
	}
	if got := <-ch; got.Kind != CardCreated || got.Slug != "standup" {
		t.Fatalf("change = %+v", got)
	}
	if _, err := s.Create("Standup!", "x", "a", "p", mustSch(t, "daily 09:00")); err == nil {
		t.Error("bad slug must refuse")
	}
	if _, err := s.Create("standup", "x", "a", "p", mustSch(t, "daily 09:00")); err == nil {
		t.Error("duplicate slug must refuse")
	}
	if _, err := s.Create("blank", "", "a", "p", mustSch(t, "daily 09:00")); err == nil {
		t.Error("empty name must refuse")
	}
	re, err := Open(s.ws)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(re.List()) != 1 {
		t.Fatalf("reopen cards = %d, want 1", len(re.List()))
	}
}

func TestSetEnabledAndRemove(t *testing.T) {
	s := withStore(t, nil)
	ch, cancel := s.Subscribe()
	defer cancel()
	_, _ = s.Create("a", "A", "agent", "p", mustSch(t, "interval 10m"))
	<-ch
	if err := s.SetEnabled("a", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	c, _ := s.Get("a")
	if c.Enabled {
		t.Error("card still enabled after SetEnabled(false)")
	}
	if len(s.Due(clock(2026, 9, 2, 8, 0))) != 0 {
		t.Error("paused card must not be due")
	}
	if err := s.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := s.Get("a"); ok {
		t.Error("card still listed after Remove")
	}
	re, err := Open(s.ws)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(re.List()) != 0 {
		t.Fatalf("reopen cards = %d, want 0", len(re.List()))
	}
}

func TestNextArm(t *testing.T) {
	ws := &workspace.Workspace{Root: t.TempDir()}
	s, _ := Open(ws)
	now := clock(2026, 9, 2, 8, 0)

	if _, ok := s.NextArm(now); ok {
		t.Error("empty store must not arm")
	}

	_, _ = s.Create("i", "I", "a", "p", mustSch(t, "interval 10m"))
	next, ok := s.NextArm(now)
	if !ok || !next.Equal(now) {
		t.Errorf("interval never-ran NextArm = %v,%v want %v,true", next, ok, now)
	}
	if err := s.MarkRan("i", now); err != nil {
		t.Fatal(err)
	}
	next, _ = s.NextArm(now)
	if want := now.Add(10 * time.Minute); !next.Equal(want) {
		t.Errorf("interval NextArm = %v, want %v", next, want)
	}

	_ = s.SetEnabled("i", false)
	_, _ = s.Create("d", "D", "a", "p", mustSch(t, "daily 09:00"))
	next, ok = s.NextArm(now)
	if !ok || !next.Equal(clock(2026, 9, 2, 9, 0)) {
		t.Errorf("daily NextArm = %v,%v want 09:00,true", next, ok)
	}
	if err := s.MarkRan("d", clock(2026, 9, 2, 9, 0)); err != nil {
		t.Fatal(err)
	}
	next, _ = s.NextArm(clock(2026, 9, 2, 9, 0))
	if want := clock(2026, 9, 3, 9, 0); !next.Equal(want) {
		t.Errorf("daily NextArm after run = %v, want %v", next, want)
	}

	_, _ = s.Create("p", "P", "a", "p", mustSch(t, "interval 5m"))
	_ = s.SetEnabled("d", false)
	_ = s.SetEnabled("p", false)
	if _, ok := s.NextArm(now); ok {
		t.Error("all-paused store must not arm")
	}
}

func mustSch(t *testing.T, s string) Schedule {
	t.Helper()
	sch, err := ParseSchedule(s)
	if err != nil {
		t.Fatalf("ParseSchedule(%q): %v", s, err)
	}
	return sch
}
