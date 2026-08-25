// Package profile aggregates everything DHI knows about one agent into
// a read-only inspection view (F-003 component 4): manifest inventory,
// open tasks with ChangeSet state, recent channel activity, private
// memory, knowledge-base contributions, and the coding standards
// currently layered onto its turns. Sources degrade independently — a
// missing journal or KB never fails the whole profile.
package profile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Roster is the narrow seam profiles need from the runtime (satisfied
// by *runtime.Runtime): live agent ids and their parsed manifests.
type Roster interface {
	AgentIDs() []string
	Manifest(id string) (*manifest.Agent, bool)
}

// Deps wires every optional source; nil sources yield empty sections.
type Deps struct {
	Roster    Roster
	Bus       *bus.Bus
	Org       *org.Org
	Tasks     *tasks.Store
	Memory    *memory.Store
	KB        KBSearch
	Standards bool // resolve standards blocks for the agent
}

// KBSearch is the slice of the knowledge store profiles render.
type KBSearch interface {
	ContributionsBy(authorID string) []knowledge.Entry
}

// Profile is the aggregated read-only view.
type Profile struct {
	ID       string
	Found    bool // on the active roster (vs archived/unknown)
	Manifest *manifest.Agent

	Teams []string

	TasksOpen []tasks.Task
	TasksDone []tasks.Task

	Journal  []memory.Entry
	Notes    string
	NotesErr error

	KBAuthor []knowledge.Entry

	StandardsBlock string

	RecentActivity []bus.Message // newest first, capped
}

// activityCap bounds the timeline section.
const activityCap = 20

// Build assembles the profile for id from every wired source.
func Build(ws *workspace.Workspace, d Deps, id string) *Profile {
	p := &Profile{ID: id}
	if d.Roster != nil {
		if m, ok := d.Roster.Manifest(id); ok {
			p.Found = true
			p.Manifest = m
		}
	}
	if d.Org != nil {
		p.Teams = d.Org.TeamsOf(id)
	}
	if d.Tasks != nil {
		for _, t := range d.Tasks.List() {
			if t.Assignee != id {
				continue
			}
			if t.Status == tasks.Done {
				p.TasksDone = append(p.TasksDone, t)
			} else {
				p.TasksOpen = append(p.TasksOpen, t)
			}
		}
	}
	if d.Memory != nil {
		j, err := d.Memory.Journal(id, 50)
		if err == nil {
			p.Journal = j
		}
		notes, nerr := d.Memory.ReadNotes(id)
		if nerr == nil {
			p.Notes = notes
		} else if !osIsNotExist(nerr) {
			p.NotesErr = nerr
		}
	}
	if d.KB != nil {
		p.KBAuthor = d.KB.ContributionsBy(id)
	}
	if d.Standards && ws != nil {
		p.StandardsBlock = standards.Resolve(ws.Root, id, teamLookup(d.Org))
	}
	if d.Bus != nil && ws != nil {
		p.RecentActivity = recentActivity(d.Bus, id)
	}
	return p
}

func teamLookup(o *org.Org) func(string) []string {
	if o == nil {
		return nil
	}
	return o.TeamsOf
}

// recentActivity scans every channel for messages authored by id,
// newest first. Channels are cheap JSONL reads here; volume is bounded
// by activityCap per channel before the global cap.
func recentActivity(b *bus.Bus, id string) []bus.Message {
	channels := b.Channels()
	var out []bus.Message
	for _, ch := range channels {
		hist := b.History(ch, 0)
		n := len(hist)
		start := n - activityCap
		if start < 0 {
			start = 0
		}
		for _, m := range hist[start:] {
			if m.Author == id {
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > activityCap {
		out = out[:activityCap]
	}
	return out
}

func osIsNotExist(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such file")
}

// ---- rendering helpers shared with the UI section ----

// Summary renders the one-line identity used in lists.
func (p *Profile) Summary() string {
	if !p.Found || p.Manifest == nil {
		return "(not on roster)"
	}
	tools := "no tools"
	if n := len(p.Manifest.Tools); n > 0 {
		tools = fmt.Sprintf("%d tool(s)", n)
	}
	return fmt.Sprintf("%s · %s · %s", p.Manifest.Name, p.Manifest.Model, tools)
}

// TaskLine renders current-work state for dashboards.
func (p *Profile) TaskLine() string {
	if len(p.TasksOpen) == 0 {
		return "idle"
	}
	t := p.TasksOpen[0]
	extra := ""
	if len(t.ChangeSets) > 0 {
		members := make([]string, 0, len(t.ChangeSets))
		for _, cs := range t.ChangeSets {
			members = append(members, cs.Member)
		}
		extra = " [" + strings.Join(members, ",") + "]"
	}
	return fmt.Sprintf("%s (%s)%s", t.Slug, t.Status, extra)
}
