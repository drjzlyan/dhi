package workflow

import "strings"

// Progress reports what a feature task has actually achieved so far.
// The gate evaluator compares a workflow's block/approval steps against
// this; DHI owns every field (worktree attach, commit, push, PR, the
// declared test command), so none of it is self-reported by the agent.
type Progress struct {
	Worktree  bool // a worktree is attached to the task
	Committed bool // a commit landed on the worktree branch
	Pushed    bool // the branch was pushed
	PR        bool // a pull request is open
	Reviewed  bool // the review step was approved (or bypass recorded)
	TestsPass bool // the declared test command last passed
}

// Verdict is one unmet step relevant to a seam action.
type Verdict struct {
	Step   Step
	Reason string
}

// Satisfied reports whether a step's condition holds for p.
func Satisfied(s Step, p Progress) bool {
	switch {
	case s.Bind == SeamWorktree:
		return p.Worktree
	case s.Bind == SeamCommit:
		return p.Committed
	case s.Bind == SeamPush:
		return p.Pushed
	case s.Bind == SeamPR:
		return p.PR
	case s.Bind == SeamReview:
		return p.Reviewed
	case strings.HasPrefix(s.Bind, RunPrefix):
		return p.TestsPass
	}
	return false
}

// prereqs maps a seam action to the seam(s) whose block/approval steps
// guard it (ADR-0020 §3): a commit needs the worktree step cleared, a PR
// needs the declared test to pass, a review needs its own approval. A
// seam with no entry is guarded by its own step.
var prereqs = map[string][]string{
	SeamCommit: {SeamWorktree},       // worktree-before-commit
	SeamPush:   {SeamWorktree},       // push rides the worktree branch
	SeamPR:     {RunPrefix + "test"}, // tests-pass-before-PR
}

// CheckGate returns the unmet block / exception-approval steps that must
// be cleared before acting on seam, in workflow order. Only the steps
// guarding this specific action are considered, so committing is not
// blocked by an unrelated later step. A workflow that declares no such
// step has no opinion (nil).
func (d *Definition) CheckGate(seam string, p Progress) []Verdict {
	need := prereqs[seam]
	if need == nil {
		need = []string{seam}
	}
	var out []Verdict
	for _, s := range d.Steps {
		if s.Gate == GateWarn || Satisfied(s, p) {
			continue
		}
		for _, n := range need {
			if s.Bind == n {
				out = append(out, Verdict{Step: s, Reason: reason(s)})
				break
			}
		}
	}
	return out
}

func reason(s Step) string {
	switch {
	case s.Gate == GateApprove:
		return "step " + s.ID + " needs an explicit approval: " + s.Title
	case s.Bind == SeamWorktree:
		return "step " + s.ID + " requires a worktree first: " + s.Title
	case strings.HasPrefix(s.Bind, RunPrefix):
		return "step " + s.ID + " requires " + s.Bind + " to pass: " + s.Title
	default:
		return "step " + s.ID + " is unmet: " + s.Title
	}
}

// Render formats a workflow's guidance as the prompt block appended to an
// agent's system prompt for a feature turn. Only guidance and titles ride
// here; the gates are enforced at their seams, not promised to the model.
func Render(d *Definition) string {
	var b strings.Builder
	b.WriteString("Feature workflow (")
	b.WriteString(d.Slug)
	b.WriteString("): follow these steps in order. DHI enforces the blocked steps at their seam.")
	for _, s := range d.Steps {
		b.WriteString("\n- ")
		if s.Title != "" {
			b.WriteString(s.Title)
		} else {
			b.WriteString(s.ID)
		}
		if s.Gate != GateWarn {
			b.WriteString(" [enforced]")
		}
		if g := strings.TrimSpace(s.Guidance); g != "" {
			b.WriteString(": ")
			b.WriteString(g)
		}
	}
	return b.String()
}
