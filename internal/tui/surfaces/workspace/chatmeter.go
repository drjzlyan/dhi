package workspace

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// F-127: the DM header's context and cost meter. An episode already
// holds the latest prompt size and the spend; this only reads it.

// meterTTL keeps redraws from re-reading episode files.
const meterTTL = time.Second

// chatMeter is the pane's read seam onto the episodes. The limits are
// live (a Settings edit applies at once).
type chatMeter struct {
	episodes *episode.Store
	limits   func() (prompt, session int)

	at   time.Time
	key  string
	text [2]string // full, compact (no tokens)
}

// meterEpisode picks the episode a DM header speaks for: the open
// thread's, else the DM's most recently updated one.
func meterEpisode(st *episode.Store, ch string, thread int64) (episode.Episode, bool) {
	agent := bus.ChannelLabel(ch)
	if thread != 0 {
		ep, ok, err := st.Get(agent, episode.Key("", ch, thread))
		return ep, ok && err == nil
	}
	eps, err := st.List(agent)
	if err != nil {
		return episode.Episode{}, false
	}
	var best episode.Episode
	found := false
	for _, e := range eps {
		if !strings.HasPrefix(e.Key, "thread:"+ch+":") {
			continue
		}
		if !found || e.Updated.After(best.Updated) {
			best, found = e, true
		}
	}
	return best, found
}

// forChat renders the meter for a conversation: full and compact forms,
// both "" when there is nothing to show (channels, no episode yet).
func (c *chatMeter) forChat(ch string, thread int64, now time.Time) (full, compact string) {
	if c == nil || c.episodes == nil || !bus.IsDM(ch) {
		return "", ""
	}
	key := fmt.Sprintf("%s|%d", ch, thread)
	if c.key == key && now.Sub(c.at) < meterTTL {
		return c.text[0], c.text[1]
	}
	c.key, c.at, c.text = key, now, [2]string{}
	if ep, ok := meterEpisode(c.episodes, ch, thread); ok {
		var prompt, session int
		if c.limits != nil {
			prompt, session = c.limits()
		}
		c.text = [2]string{
			joinMeter(modelChip(ep.Model), meterText(ep, prompt, session, true)),
			joinMeter(modelChip(ep.Model), meterText(ep, prompt, session, false)),
		}
	}
	return c.text[0], c.text[1]
}

// meterText is pure for tests. A kept CLI session resumes at the last
// context, so it is bounded by session_tokens; otherwise the prompt is
// rebuilt each turn and prompt_tokens is the budget.
func meterText(ep episode.Episode, promptLimit, sessionLimit int, tokens bool) string {
	if ep.Turns == 0 {
		return ""
	}
	limit := promptLimit
	if ep.Session != "" {
		limit = sessionLimit
	}
	var parts []string
	if ep.LastContext > 0 {
		s := "ctx " + tokenCount(ep.LastContext)
		style := theme.TextDim()
		if limit > 0 {
			pct := ep.LastContext * 100 / limit
			s += fmt.Sprintf("/%s %d%%", tokenCount(limit), pct)
			switch {
			case pct >= 100:
				style = theme.DangerText()
			case pct >= 80:
				style = theme.WarningText()
			}
		}
		parts = append(parts, style.Render(s))
	}
	if tokens && ep.TokensIn+ep.TokensOut > 0 {
		parts = append(parts, theme.TextDim().Render(tokenCount(ep.TokensIn)+" in "+tokenCount(ep.TokensOut)+" out"))
	}
	if ep.CostUSD > 0 {
		parts = append(parts, theme.TextDim().Render(fmtCost(ep.CostUSD)))
	}
	return strings.Join(parts, theme.Hint().Render(" · "))
}

// tokenCount renders 18400 as "18k", 1250000 as "1.2M".
func tokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

// withMeter appends the meter to a header if it fits width: the full
// form, else the compact one, else nothing. width <= 0 means no limit.
func withMeter(header, full, compact string, width int) string {
	for _, m := range []string{full, compact} {
		if m == "" {
			continue
		}
		if h := header + "  " + m; width <= 0 || lipgloss.Width(h) <= width {
			return h
		}
	}
	return header
}

// modelChip marks a conversation running on a model override (F-130).
func modelChip(model string) string {
	if model == "" {
		return ""
	}
	return theme.AccentText().Render("model " + model)
}

func joinMeter(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + theme.Hint().Render(" · ") + b
}

// forget drops the cached text so the next draw re-reads the episode.
func (c *chatMeter) forget() {
	if c != nil {
		c.key = ""
	}
}
