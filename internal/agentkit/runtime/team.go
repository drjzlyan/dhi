package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-071: a team that runs itself — leads split work into dependent
// sub-cards, DHI releases each brief when its blockers finish, hand-offs
// carry the hander's working state, and a reviewer agent reviews a PR
// before it reaches the human.

// handoff posts a card's brief to its assignee (team channel, else DM),
// binds the card to that message, and dispatches it. author is who hands
// it over (DHI itself for a dependency release).
func (r *Runtime) handoff(ctx context.Context, t tasks.Task, author, why string) {
	if t.Assignee == "" || t.Assignee == bus.Human || r.cfg.Bus == nil {
		return
	}
	ch := "dm:" + t.Assignee
	if t.Team != "" {
		ch = "#" + t.Team
	}
	text := fmt.Sprintf("@%s task %s: %s", t.Assignee, t.Slug, t.Title)
	if why != "" {
		text += " — " + why
	}
	posted, err := r.cfg.Bus.Post(bus.Message{Channel: ch, Author: author, Text: text})
	if err != nil {
		return
	}
	_ = r.cfg.Tasks.BindThread(t.Slug, posted.Channel, posted.ID)
	go r.Handle(context.WithoutCancel(ctx), posted)
}

// ReleaseReady hands off every held card whose dependencies are now done.
// It runs after each turn and whenever a card changes (WatchTasks).
func (r *Runtime) ReleaseReady(ctx context.Context) {
	if r.cfg.Tasks == nil {
		return
	}
	for _, t := range r.cfg.Tasks.Ready() {
		if won, err := r.cfg.Tasks.Release(t.Slug); err != nil || !won {
			continue // another releaser handed it off
		}
		r.handoff(ctx, t, systemAuthor, "unblocked: "+strings.Join(t.DependsOn, ", ")+" done")
	}
}

// WatchTasks releases held cards as their blockers finish, whoever moved
// them (an agent's task_status or the human on the board). It returns
// when ctx ends.
func (r *Runtime) WatchTasks(ctx context.Context) {
	if r.cfg.Tasks == nil {
		return
	}
	ch, cancel := r.cfg.Tasks.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-ch:
			if c.Kind == tasks.TaskUpdated {
				r.ReleaseReady(ctx)
			}
		}
	}
}

// teamBlock tells a card's assignee about the decomposition around it:
// a lead gets the roster and the invitation to split; a parent shows its
// sub-cards; a sub-card names its parent and blockers.
func (r *Runtime) teamBlock(agentID string, t tasks.Task) string {
	var b strings.Builder
	if kids := r.cfg.Tasks.Children(t.Slug); len(kids) > 0 {
		done := 0
		var parts []string
		for _, k := range kids {
			mark := string(k.Status)
			if k.Status == tasks.Done {
				done++
				mark = "done"
			}
			parts = append(parts, fmt.Sprintf("%s (%s, %s)", k.Slug, orNobody(k.Assignee), mark))
		}
		fmt.Fprintf(&b, "\n- sub-tasks %d/%d done: %s", done, len(kids), strings.Join(parts, "; "))
		b.WriteString("\n- this card tracks its sub-tasks: each one ships its own PR; this card has no branch or PR of its own. " +
			"Coordinate, unblock, and move it to done when every sub-task is done")
	} else if t.Parent == "" && r.cfg.Org != nil && t.Team != "" {
		if team, ok := r.cfg.Org.Team(t.Team); ok && team.Lead == agentID {
			var roster []string
			for _, m := range team.Members {
				if m == agentID {
					continue
				}
				role := "member"
				if e, ok := r.agentEntry(m); ok && e.m.Role != "" {
					role = e.m.Role
				}
				roster = append(roster, m+" ("+role+")")
			}
			if len(roster) > 0 {
				fmt.Fprintf(&b, "\n- you lead team %s: %s. If this is more than one person's work, split it with task_create "+
					"(parent=%q, an assignee each, depends_on for ordering — DHI hands each brief off when its blockers are done); "+
					"otherwise do it yourself", t.Team, strings.Join(roster, ", "), t.Slug)
			}
		}
	}
	if t.Parent != "" {
		fmt.Fprintf(&b, "\n- part of %s", t.Parent)
		if bl := r.cfg.Tasks.Blockers(t.Slug); len(bl) > 0 {
			fmt.Fprintf(&b, "; still waiting on %s", strings.Join(bl, ", "))
		}
	}
	return b.String()
}

func orNobody(s string) string {
	if s == "" {
		return "unassigned"
	}
	return s
}

// agentEntry looks up a rostered agent.
func (r *Runtime) agentEntry(id string) (*entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.agents[id]
	return e, ok
}

// handoffBrief gives an agent woken by another agent the hander's working
// state on this work — goal, plan, verify verdict, last outcome — instead
// of making it reconstruct that from prose.
func (r *Runtime) handoffBrief(agentID string, trigger bus.Message) string {
	from := trigger.Author
	if from == agentID || !r.isAgent(from) || r.cfg.Episodes == nil {
		return ""
	}
	task, _ := r.boundTask(trigger)
	ep, ok, err := r.cfg.Episodes.Get(from, episode.Key(task.Slug, trigger.Channel, bus.ThreadOf(trigger)))
	if err != nil || !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nHanded over by @%s. Their working state:", from)
	if ep.Goal != "" {
		b.WriteString("\n- goal: " + truncateRunes(ep.Goal, 300))
	}
	for _, s := range ep.Plan {
		fmt.Fprintf(&b, "\n- [%s] %s", s.Status, s.Title)
	}
	if ep.Verify != "" {
		fmt.Fprintf(&b, "\n- DHI verify: %s %s", ep.Verify, ep.VerifyCmd)
	}
	if ep.LastSummary != "" {
		b.WriteString("\n- their last outcome: " + truncateRunes(ep.LastSummary, 400))
	}
	return b.String()
}

// reviewerFor picks the agent that reviews author's PRs: a rostered agent
// with the "reviewer" role, preferring one on the card's team; never the
// author. "" = nobody, and the PR goes straight to the human.
func (r *Runtime) reviewerFor(author string, t tasks.Task) string {
	r.mu.Lock()
	var cands []string
	for id, e := range r.agents {
		if id != author && e.m.Role == "reviewer" {
			cands = append(cands, id)
		}
	}
	r.mu.Unlock()
	sort.Strings(cands)
	if len(cands) == 0 {
		return ""
	}
	if r.cfg.Org != nil && t.Team != "" {
		if team, ok := r.cfg.Org.Team(t.Team); ok {
			for _, c := range cands {
				for _, m := range team.Members {
					if m == c {
						return c
					}
				}
			}
		}
	}
	return cands[0]
}

// prOpened asks a reviewer agent to review the PR in the card's thread
// before the human does (F-071). Its findings land in the same thread,
// where the author's next turn reads them — and learns from them.
func (r *Runtime) prOpened(ctx context.Context, author string, trigger bus.Message) func(string, []string) {
	return func(slug string, opened []string) {
		t, ok := r.cfg.Tasks.Get(slug)
		if !ok {
			return
		}
		rev := r.reviewerFor(author, t)
		if rev == "" {
			return
		}
		posted, err := r.cfg.Bus.Post(bus.Message{
			Channel: trigger.Channel, Thread: bus.ThreadOf(trigger), Author: author,
			Text: fmt.Sprintf("@%s please review the PR for task %s (%s) before it goes to the human. "+
				"Reply to @%s with concrete findings (file:line), or tell @%s it is ready.", rev, slug, strings.Join(opened, "; "), author, author),
		})
		if err == nil {
			go r.Handle(context.WithoutCancel(ctx), posted)
		}
	}
}

// prHook is the pr_open follow-up for a turn (nil without a task store).
func (r *Runtime) prHook(author string, trigger bus.Message) func(string, []string) {
	if r.cfg.Tasks == nil || r.cfg.Bus == nil {
		return nil
	}
	return r.prOpened(context.Background(), author, trigger)
}

// childGate refuses a PR from a tracking card (one with sub-tasks): each
// sub-task ships its own PR from its own branch.
func (r *Runtime) childGate(trigger bus.Message) func(string) []string {
	return func(seam string) []string {
		if seam != workflow.SeamPR || r.cfg.Tasks == nil {
			return nil
		}
		t, ok := r.boundTask(trigger)
		if !ok {
			return nil
		}
		if kids := r.cfg.Tasks.Children(t.Slug); len(kids) > 0 {
			return []string{"this card tracks sub-tasks; each sub-task opens its own PR"}
		}
		return nil
	}
}

// isTracking reports a card with sub-tasks: it gets no worktree and no
// verify of its own (F-071).
func (r *Runtime) isTracking(t tasks.Task) bool {
	return r.cfg.Tasks != nil && t.Slug != "" && len(r.cfg.Tasks.Children(t.Slug)) > 0
}
