package workspace

import (
	"fmt"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/profile"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Agent profile (F-022): the INSPECT profile re-hosted as the CHANNELS
// context pane's profile mode. Sources degrade independently.

var (
	memCache *memory.Store
	kbCache  *knowledge.Store
	cacheWS  string
)

// store caches built once per workspace root (inspection reads only).
func memoryStoreFor(ws *workspace.Workspace) *memory.Store {
	if ws == nil || cacheWS == ws.Root && memCache != nil {
		return memCache
	}
	memCache = memory.Open(ws)
	kb, err := knowledge.Open(ws, knowledge.Auto, nil)
	if err == nil {
		kbCache = kb
	}
	cacheWS = ws.Root
	return memCache
}

func kbStoreFor(ws *workspace.Workspace) profile.KBSearch {
	memoryStoreFor(ws)
	return kbCache
}

// agentProfileLines renders the profile block for the context pane.
func (m *Model) agentProfileLines(id string) []string {
	if m.ws == nil {
		return nil
	}
	deps := profile.Deps{
		Roster: m.roster, Bus: m.bus, Org: m.org,
		Tasks:  m.taskStore,
		Memory: memoryStoreFor(m.ws), KB: kbStoreFor(m.ws),
		Standards: true,
	}
	p := profile.Build(m.ws, deps, id)
	return compactProfileLines(p)
}

func compactProfileLines(p *profile.Profile) []string {
	var out []string
	add := func(s string) {
		if len(out) < 30 {
			out = append(out, s)
		}
	}
	if p.Manifest != nil {
		sys := p.Manifest.System
		if len(sys) > 40 {
			sys = sys[:37] + "…"
		}
		add("model " + p.Manifest.Model +
			theme.Hint().Render(" tools: "+orDash(strings.Join(p.Manifest.Tools, ","))))
		if sys != "" {
			add(theme.TextDim().Render(sys))
		}
	} else {
		add(theme.TextDim().Render("(not on the active roster)"))
	}
	if len(p.Teams) > 0 {
		add("teams " + theme.TabActive().Render(strings.Join(p.Teams, ", ")))
	}
	add(fmt.Sprintf("tasks %d open · %d done", len(p.TasksOpen), len(p.TasksDone)))
	for _, t := range p.TasksOpen {
		if len(out) >= 30 {
			break
		}
		add(theme.Hint().Render("◦ " + t.Slug + " · " + string(t.Status)))
	}
	if runs := profileRuns(p); len(runs) > 0 {
		rl := tasks.RollupRuns(runs)
		add(theme.TextDim().Render(rl.Summary()))
		for i, mr := range tasks.RollupByModel(runs) {
			if i >= 4 || len(out) >= 30 {
				break
			}
			add(theme.Hint().Render("  " + mr.Line()))
		}
	}
	add("")
	add(theme.Hint().Render("recent"))
	for i, msg := range p.RecentActivity {
		if i >= 4 || len(out) >= 30 {
			break
		}
		text := msg.Text
		if len(text) > 26 {
			text = text[:23] + "…"
		}
		add(" " + theme.Hint().Render(msg.Channel) + " " + text)
	}
	return out
}

// profileRuns collects every run recorded by the profile's agent across
// its tasks, newest first (F-014 RUNS subsection).
func profileRuns(p *profile.Profile) []tasks.Run {
	var runs []tasks.Run
	for _, t := range p.TasksOpen {
		for _, r := range t.Runs {
			if r.Agent == p.ID {
				runs = append(runs, r)
			}
		}
	}
	for _, t := range p.TasksDone {
		for _, r := range t.Runs {
			if r.Agent == p.ID {
				runs = append(runs, r)
			}
		}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		return runs[i].Started.After(runs[j].Started)
	})
	return runs
}

var _ = bus.Human
