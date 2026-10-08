package reviewer

import (
	"context"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// submitDraft is the review being composed in the submit dialog: what the
// user chose and exactly what that will send (F-049).
type submitDraft struct {
	id      string
	verdict review.Verdict
	summary string
	plan    review.SubmitPlan
}

var verdictLabels = []string{"comment", "approve", "request changes"}

// openSubmit starts the submit flow for the open review (or the one
// selected in REVIEWS). A PR-backed review opens the verdict dialog; any
// other review has nowhere to go, so its drafts are simply finalized.
func (m *Model) openSubmit() {
	if m.svc == nil {
		m.opErr = "review service unavailable"
		return
	}
	var r review.Review
	if cur, ok := m.openReview(); ok {
		r = cur
	} else if sel := selReview(m.reviews(), m.cursors[secReviews]); sel != nil {
		r = *sel
	} else {
		m.opErr = "no review selected"
		return
	}
	if r.Target.PRNumber <= 0 {
		if r.PendingCount() == 0 {
			m.opErr = "nothing to submit — add a comment first"
			return
		}
		n := r.PendingCount()
		if err := m.svc.Store().Submit(r.ID); err != nil {
			m.opErr = err.Error()
			return
		}
		m.closeFormWithFlash("finalized " + r.ID + " (" + itoa(n) + " comments) — no PR to send it to")
		return
	}
	if !m.svc.HasGH() {
		m.opErr = "gh unavailable — install the GitHub CLI to send reviews"
		return
	}
	verdict := field{label: "verdict", toggle: verdictLabels}
	m.form = formState{kind: fSubmit, orig: r.ID, fields: []field{verdict, textField("summary", r.Summary)}}
	m.draft = nil
}

// planSubmit reads the dialog and builds the confirmation.
func (m *Model) planSubmit() {
	f := &m.form
	r, ok := m.svc.Store().Get(f.orig)
	if !ok {
		f.err = "review vanished"
		return
	}
	v, err := review.ParseVerdict(f.fields[0].toggleValue())
	if err != nil {
		f.err = err.Error()
		return
	}
	summary := strings.TrimSpace(f.fields[1].text())
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	plan, err := m.svc.PlanReview(ctx, r, v, summary)
	if err != nil {
		f.err = err.Error()
		return
	}
	if v == review.VerdictComment && plan.Body == "" && len(plan.Inline) == 0 && len(plan.Replies) == 0 {
		f.err = "nothing to send — write a comment or a summary (or approve / request changes)"
		return
	}
	f.err = ""
	m.draft = &submitDraft{id: r.ID, verdict: v, summary: summary, plan: plan}
	f.kind = fSubmitConfirm
}

// sendSubmit sends the confirmed review off the UI loop.
func (m *Model) sendSubmit() {
	d := m.draft
	if d == nil || m.svc == nil {
		return
	}
	r, ok := m.svc.Store().Get(d.id)
	if !ok {
		return
	}
	m.closeForm()
	m.busy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		plan, url, err := m.svc.SubmitReview(ctx, r, d.verdict, d.summary)
		ev := revEvent{kind: evSubmitted, id: d.id, err: errString(err), n: plan.InlineCount() + plan.ReplyCount(), url: url}
		m.send(ev)
	}()
}

// submitLines renders the dialog bodies.
func (m *Model) submitLines() []string {
	f := &m.form
	switch f.kind {
	case fSubmit:
		lines := []string{
			theme.TextDim().Render("one review, sent as you — nothing carries an employee's name"),
			fieldLine(f.fields[0], f.cur == 0),
			theme.TextDim().Render("      ←/→ approve · request changes · or just comment"),
			fieldLine(f.fields[1], f.cur == 1),
			"",
		}
		if f.err != "" {
			lines = append(lines, theme.DangerText().Render(f.err), "")
		}
		return append(lines, theme.Hint().Render("tab field · enter review what will be sent · esc cancel"))
	case fSubmitConfirm:
		d := m.draft
		if d == nil {
			return []string{theme.DangerText().Render("(nothing to confirm)")}
		}
		p := d.plan
		lines := []string{theme.TextStyle().Render("This sends ONE review as you:"), "",
			"  verdict   " + theme.AccentText().Render(verdictLabel(d.verdict))}
		inline := p.InlineCount() - p.InBody
		lines = append(lines, "  inline    "+plural(inline, "comment")+" on the diff")
		if p.InBody > 0 {
			lines = append(lines, "  in text   "+plural(p.InBody, "note")+" folded into the summary")
		}
		if n := p.ReplyCount(); n > 0 {
			lines = append(lines, "  replies   "+plural(n, "reply", "replies")+" in existing threads")
		}
		if p.Body != "" {
			lines = append(lines, "", theme.TextDim().Render("summary:"))
			for _, l := range kit.WrapWords(p.Body, 56) {
				lines = append(lines, "  "+theme.TextStyle().Render(l))
				if len(lines) > 16 {
					lines = append(lines, theme.TextDim().Render("  …"))
					break
				}
			}
		}
		if p.Suggested > 0 {
			lines = append(lines, "", theme.WarningText().Render(
				plural(p.Suggested, "suggestion")+" from your team undecided — NOT sent"))
		}
		if f.err != "" {
			lines = append(lines, "", theme.DangerText().Render(f.err))
		}
		return append(lines, "", theme.Hint().Render("enter send · esc back and edit"))
	}
	return nil
}

func verdictLabel(v review.Verdict) string {
	switch v {
	case review.VerdictApprove:
		return "approve"
	case review.VerdictRequestChanges:
		return "request changes"
	}
	return "comment"
}

func plural(n int, one string, many ...string) string {
	word := one + "s"
	if len(many) > 0 {
		word = many[0]
	}
	if n == 1 {
		word = one
	}
	return strconv.Itoa(n) + " " + word
}
