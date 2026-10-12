package workspace

import (
	"charm.land/lipgloss/v2"

	"fmt"

	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// episodeLines is the card pane's live-run block (F-067): the assignee's
// published plan, DHI's own verify verdict, and what the work has cost.
// Nothing renders until the assignee has an episode on this card.
func (m *Model) episodeLines(tk tasks.Task, wrapW int) []string {
	if m.episodes == nil || tk.Assignee == "" {
		return nil
	}
	ep, ok, err := m.episodes.Get(tk.Assignee, episode.Key(tk.Slug, "", 0))
	if err != nil || !ok {
		return nil
	}
	return renderEpisode(ep, wrapW)
}

// renderEpisode is pure for goldens: header, plan steps, verify, spend.
func renderEpisode(ep episode.Episode, wrapW int) []string {
	var out []string
	if len(ep.Plan) > 0 {
		done := 0
		for _, s := range ep.Plan {
			if s.Status == "done" {
				done++
			}
		}
		out = append(out, theme.SectionHeader().Render("PLAN")+" "+theme.Hint().Render(fmt.Sprintf("%d/%d", done, len(ep.Plan))))
		for _, s := range ep.Plan {
			glyph, style := planGlyph(s.Status)
			for i, line := range kit.WrapWords(s.Title, clampInt(wrapW-3, 12, 70)) {
				lead := glyph
				if i > 0 {
					lead = " "
				}
				out = append(out, " "+lead+" "+style.Render(line))
			}
		}
	}
	if v := verifyLine(ep); v != "" {
		out = append(out, v)
	}
	if ep.Turns > 0 {
		spend := fmt.Sprintf("%d turn", ep.Turns)
		if ep.Turns != 1 {
			spend += "s"
		}
		if ep.CostUSD > 0 {
			spend += " · " + fmtCost(ep.CostUSD)
		}
		if total := ep.TokensIn + ep.CacheRead + ep.CacheWrite; ep.CacheRead > 0 && total > 0 {
			spend += fmt.Sprintf(" · %d%% cached", ep.CacheRead*100/total)
		}
		if ep.Session != "" {
			spend += " · session kept"
		}
		out = append(out, theme.TextDim().Render(spend))
	}
	return out
}

func planGlyph(status string) (string, lipgloss.Style) {
	switch status {
	case "done":
		return theme.SuccessText().Render(theme.GlyphCheck), theme.TextDim()
	case "doing":
		return theme.WarningText().Render(theme.GlyphBusy), theme.TextStyle()
	case "blocked":
		return theme.DangerText().Render(theme.GlyphCross), theme.DangerText()
	}
	return theme.TextMuted().Render(theme.GlyphTodo), theme.TextDim()
}

// verifyLine states DHI's own check of the work, never the agent's claim.
func verifyLine(ep episode.Episode) string {
	switch ep.Verify {
	case "pass":
		return theme.SuccessText().Render(theme.GlyphCheck+" verified") + theme.Hint().Render(" · "+ep.VerifyCmd)
	case "fail":
		return theme.WarningText().Render(theme.GlyphCross+" verify failing") +
			theme.Hint().Render(fmt.Sprintf(" · %s · round %d", ep.VerifyCmd, ep.Iterations))
	case "stuck":
		return theme.DangerText().Render(theme.GlyphCross+" stuck — needs you") + theme.Hint().Render(" · "+ep.VerifyCmd)
	}
	return ""
}
