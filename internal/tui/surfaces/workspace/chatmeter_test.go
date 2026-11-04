package workspace

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func withSession(e episode.Episode) episode.Episode { e.Session = "S1"; return e }

func TestMeterText(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ep := episode.Episode{Turns: 3, LastContext: 18400, TokensIn: 12000, TokensOut: 3000, CostUSD: 0.12}
	cases := []struct {
		name   string
		ep     episode.Episode
		tokens bool
		want   string
	}{
		{"fresh uses the prompt budget", ep, true, "ctx 18k/24k 76% · 12k in 3k out · $0.12"},
		{"kept session uses the session budget", withSession(ep), true, "ctx 18k/160k 11% · 12k in 3k out · $0.12"},
		{"compact drops tokens", ep, false, "ctx 18k/24k 76% · $0.12"},
		{"no cost, no context", episode.Episode{Turns: 1, TokensIn: 5, TokensOut: 7}, true, "5 in 7 out"},
		{"no turns shows nothing", episode.Episode{}, true, ""},
	}
	for _, c := range cases {
		if got := ansi.Strip(meterText(c.ep, 24000, 160000, c.tokens)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := ansi.Strip(meterText(ep, 0, 0, false)); got != "ctx 18k · $0.12" {
		t.Errorf("unknown limit: %q", got)
	}
}

func TestMeterTiers(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	mk := func(ctx int) string {
		return meterText(episode.Episode{Turns: 1, LastContext: ctx}, 1000, 0, false)
	}
	ok, warn, danger := mk(500), mk(800), mk(1000)
	if ok == warn || warn == danger || ok == danger {
		t.Errorf("tiers should style differently: %q %q %q", ok, warn, danger)
	}
	if !strings.Contains(ansi.Strip(danger), "100%") {
		t.Errorf("danger = %q", ansi.Strip(danger))
	}
}

func TestHeaderShowsMeter(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	p, ws, _, _ := newPaneFixture(t)
	st := episode.Open(ws.Root)
	put := func(key string, ctx int, at time.Time) {
		t.Helper()
		if err := st.Put(episode.Episode{Key: key, Agent: "alice", Turns: 2, LastContext: ctx, CostUSD: 0.5, Updated: at}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	put("thread:dm:alice:7", 6000, now.Add(-time.Hour))
	put("thread:dm:alice:9", 12000, now)
	p.meter = &chatMeter{episodes: st, limits: func() (int, int) { return 24000, 160000 }}
	p.channels = []string{"#general", "dm:alice"}

	p.active = 1
	h := ansi.Strip(p.headerMeter(p.header(), 100))
	if !strings.Contains(h, "ctx 12k/24k 50%") || !strings.Contains(h, "$0.5") {
		t.Errorf("dm header = %q (want newest episode)", h)
	}
	p.meter.at = time.Time{} // drop the cache
	p.threadID = 7
	if h := ansi.Strip(p.headerMeter(p.header(), 100)); !strings.Contains(h, "ctx 6k/24k 25%") {
		t.Errorf("thread header = %q (want the thread's episode)", h)
	}
	p.threadID = 0
	p.active = 0
	if h := ansi.Strip(p.headerMeter(p.header(), 100)); strings.Contains(h, "ctx") {
		t.Errorf("channel header must carry no meter: %q", h)
	}
}

func TestMeterFitsWidth(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ep := episode.Episode{Turns: 1, LastContext: 1000, TokensIn: 1, TokensOut: 1, CostUSD: 1}
	full, compact := meterText(ep, 24000, 0, true), meterText(ep, 24000, 0, false)
	if got := ansi.Strip(withMeter("alice", full, compact, 200)); !strings.Contains(got, "1 in 1 out") {
		t.Errorf("wide: %q", got)
	}
	n := len(ansi.Strip("alice  " + compact))
	if got := ansi.Strip(withMeter("alice", full, compact, n)); !strings.Contains(got, "ctx") || strings.Contains(got, "out") {
		t.Errorf("compact: %q", got)
	}
	if got := ansi.Strip(withMeter("alice", full, compact, 8)); got != "alice" {
		t.Errorf("tight: %q", got)
	}
}
