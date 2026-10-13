package workspace

import (
	"fmt"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Perf is an agent's employee record (F-071 Agent HQ): what it shipped,
// how reliably, at what cost, and how much it has grown.
type Perf struct {
	Runs, RunsOK       int
	TasksDone          int
	Verified, Episodes int // episodes DHI verified as passing, of all
	Spend7             [7]float64
	SpendWeek          float64
	Lessons, Pinned    int
	Tools              int // forged tools this agent proposed that are active
}

// buildPerf folds the stores into one agent's record (pure over inputs).
func buildPerf(agent string, runs []tasks.Run, done int, eps []episode.Episode, ledger []budget.Entry,
	ls []lessons.Lesson, forged []forge.Tool, now time.Time) Perf {
	p := Perf{TasksDone: done, Episodes: len(eps)}
	for _, r := range runs {
		p.Runs++
		if r.Status == tasks.RunOK {
			p.RunsOK++
		}
	}
	for _, e := range eps {
		if e.Verify == "pass" {
			p.Verified++
		}
	}
	today := budget.DayStart(now)
	for _, e := range ledger {
		if e.Agent != agent {
			continue
		}
		day := int(today.Sub(budget.DayStart(e.At)).Hours() / 24)
		if day >= 0 && day < 7 {
			p.Spend7[6-day] += e.CostUSD
			p.SpendWeek += e.CostUSD
		}
	}
	for _, l := range ls {
		if l.Scope == lessons.ScopePrivate && l.Status != lessons.StatusRetired {
			p.Lessons++
			if l.Status == lessons.StatusPinned {
				p.Pinned++
			}
		}
	}
	for _, t := range forged {
		if t.ProposedBy == agent {
			p.Tools++
		}
	}
	return p
}

// perfLines renders the record for the profile pane.
func perfLines(p Perf) []string {
	out := []string{theme.SectionHeader().Render("PERFORMANCE")}
	rate := "—"
	if p.Runs > 0 {
		rate = fmt.Sprintf("%d%%", p.RunsOK*100/p.Runs)
	}
	out = append(out, fmt.Sprintf("%s shipped · %s runs ok · %s verified",
		theme.SuccessText().Render(fmt.Sprintf("%d task(s)", p.TasksDone)), rate,
		fmt.Sprintf("%d/%d", p.Verified, p.Episodes)))
	perTask := ""
	if p.TasksDone > 0 && p.SpendWeek > 0 {
		perTask = theme.Hint().Render(fmt.Sprintf(" · ~%s/task", fmtCost(p.SpendWeek/float64(p.TasksDone))))
	}
	out = append(out, theme.AccentText().Render(kit.Sparkline(p.Spend7[:]))+" "+
		theme.TextDim().Render("7d "+fmtCost(p.SpendWeek))+perTask)
	growth := fmt.Sprintf("%d lesson(s)", p.Lessons)
	if p.Pinned > 0 {
		growth += fmt.Sprintf(" (%d pinned)", p.Pinned)
	}
	if p.Tools > 0 {
		growth += fmt.Sprintf(" · %d tool(s) forged", p.Tools)
	}
	return append(out, theme.TextDim().Render(growth))
}

// agentPerf gathers the record from the workspace stores.
func (m *Model) agentPerf(agent string, runs []tasks.Run, done int) Perf {
	root := m.ws.Root
	var eps []episode.Episode
	if m.episodes != nil {
		eps, _ = m.episodes.List(agent)
	}
	ledger, _ := budget.Open(root).Entries()
	ls, _ := lessons.Open(root).List(agent)
	forged, _ := forge.Open(root).Active()
	return buildPerf(agent, runs, done, eps, ledger, ls, forged, m.now())
}
