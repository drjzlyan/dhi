package main

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-182: one source of template symbols and one way to write AI parts,
// shared by the agents' tools, the board, the reviewer and the editor.

// varsForBranch are the template symbols for a branch: the task's own
// values when a card owns the branch, otherwise what the branch name
// itself gives (its last segment as slug, a ticket key found in it).
func varsForBranch(ts *tasks.Store, c conventions.Config, branch string) map[string]string {
	if ts != nil && branch != "" {
		for _, tk := range ts.List() {
			for _, cs := range tk.ChangeSets {
				if cs.Branch == branch {
					v := taskVars(ts, c, tk.Slug, cs.Member)
					v["branch"] = branch
					return v
				}
			}
		}
	}
	leaf := path.Base(branch)
	if branch == "" {
		leaf = "work"
	}
	v := branchVars(leaf)
	v["branch"], v["assignee"] = branch, ""
	if t := conventions.TicketFor(c.Commit.TicketPattern, branch); t != "" {
		v["ticket"] = t
	}
	return v
}

// taskContext is what an AI reads to name a task's branch.
func taskContext(ts *tasks.Store, slug string) string {
	if ts == nil {
		return "Task: " + slug
	}
	tk, ok := ts.Get(slug)
	if !ok {
		return "Task: " + slug
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\nTitle: %s\n", tk.Slug, tk.Title)
	if tk.Epic != "" {
		fmt.Fprintf(&b, "Epic: %s\n", tk.Epic)
	}
	if len(tk.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(tk.Labels, ", "))
	}
	return b.String()
}

// nameTaskBranch is the task branch for slug: the template filled with
// the task's symbols and fill. A nil fill means no agent is acting, so
// gen writes the AI parts (nil gen refuses by name); a non-nil fill is
// an agent's (work_branch), and a part it left out is named back to it.
func nameTaskBranch(ctx context.Context, c conventions.Config, ts *tasks.Store, slug, member string, fill map[string]string, gen conventions.Generator) (string, error) {
	vars := taskVars(ts, c, slug, member)
	branch, err := conventions.ExpandBranchFill(c.Branch.Task, vars, fill)
	m, missing := conventions.IsMissingAI(err)
	if !missing {
		return branch, err
	}
	if fill != nil {
		return "", fmt.Errorf("the branch name %w; pass fill with:\n%s", err, conventions.SlotList(m.Slots))
	}
	if gen == nil {
		return "", fmt.Errorf("the branch name needs AI-written parts and no agent can write them: %w", err)
	}
	got, gerr := gen(ctx, conventions.GenRequest{What: "git branch name", Shape: conventions.Shape(c.Branch.Task, vars),
		Slots: m.Slots, Context: taskContext(ts, slug) + "\nWrite each part as a few lower-case words; DHI joins them with dashes."})
	if gerr != nil {
		return "", fmt.Errorf("naming the branch: %w", gerr)
	}
	all := conventions.NormalizeFill(fill)
	if all == nil {
		all = map[string]string{}
	}
	for k, v := range got {
		all[k] = v
	}
	return conventions.ExpandBranchFill(c.Branch.Task, vars, all)
}

// writeCommit fills the {ai…} markers a human left in a commit message
// (the board's or the editor's draft), from diff. Text without markers
// comes back unchanged.
func writeCommit(ctx context.Context, gen conventions.Generator, text, diff string) (string, error) {
	slots := conventions.Markers(text)
	if len(slots) == 0 {
		return text, nil
	}
	if gen == nil {
		return "", fmt.Errorf("the message has AI parts ({ai:…}) and no agent can write them: write them yourself")
	}
	got, err := gen(ctx, conventions.GenRequest{What: "git commit message", Shape: conventions.Shape(text, nil),
		Slots: slots, Context: "Changes being committed:\n" + diff})
	if err != nil {
		return "", fmt.Errorf("writing the commit message: %w", err)
	}
	return conventions.FillMarkers(text, got)
}

// gitDiffFor is what a commit in dir takes, bounded for a prompt: the
// staged changes (the editor commits the index), or everything against
// HEAD (the board stages all).
func gitDiffFor(ctx context.Context, dir string, staged bool) (string, bool) {
	r := userGit()
	if r == nil {
		return "", false
	}
	which := []string{"diff", "HEAD"}
	if staged {
		which = []string{"diff", "--cached"}
	}
	stat, _, err := r.Run(ctx, dir, append(which, "--stat")...)
	if err != nil {
		return "", false
	}
	diff, _, _ := r.Run(ctx, dir, append(which, "--no-color")...)
	const max = 10000
	if len(diff) > max {
		diff = diff[:max] + "\n…"
	}
	return stat + "\n" + diff, true
}
