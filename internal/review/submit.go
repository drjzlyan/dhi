package review

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/gitdiff"
)

// Human is the author of the user's own comments.
const Human = "you"

// Verdict is the review's overall call.
type Verdict string

const (
	VerdictComment        Verdict = "comment"
	VerdictApprove        Verdict = "approve"
	VerdictRequestChanges Verdict = "request_changes"
)

// Verdicts lists the choices in display order.
func Verdicts() []Verdict { return []Verdict{VerdictComment, VerdictApprove, VerdictRequestChanges} }

// ParseVerdict accepts the stored/typed forms.
func ParseVerdict(s string) (Verdict, error) {
	switch strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, " ", "_"))) {
	case "comment", "":
		return VerdictComment, nil
	case "approve":
		return VerdictApprove, nil
	case "request_changes", "request-changes":
		return VerdictRequestChanges, nil
	}
	return "", fmt.Errorf("review: unknown verdict %q (comment, approve or request changes)", s)
}

func (v Verdict) event() string {
	switch v {
	case VerdictApprove:
		return EventApprove
	case VerdictRequestChanges:
		return EventRequestChanges
	}
	return EventComment
}

type plannedComment struct {
	ref   SentRef
	input ReviewCommentInput
}

type plannedReply struct {
	refs  []SentRef
	root  int64
	path  string
	line  int
	side  string
	texts []string
}

// SubmitPlan is exactly what a submission will send; building it has no
// side effects, so the confirmation dialog can show it truthfully.
type SubmitPlan struct {
	Event     string
	Body      string
	Inline    []plannedComment
	InBody    int // comments that could not be anchored and joined the body
	Replies   []plannedReply
	Suggested int // undecided suggestions that will NOT be sent
}

// InlineCount is the number of inline comments in the review.
func (p SubmitPlan) InlineCount() int { return len(p.Inline) }

// ReplyCount is the number of replies to existing GitHub threads.
func (p SubmitPlan) ReplyCount() int {
	n := 0
	for _, r := range p.Replies {
		n += len(r.refs)
	}
	return n
}

// anchors indexes which (path, side, line) the diff can carry a comment on.
type anchors map[string]map[Side]map[int]bool

func buildAnchors(files []gitdiff.FileDiff) anchors {
	a := anchors{}
	for _, f := range files {
		p := f.DisplayPath()
		a[p] = map[Side]map[int]bool{SideNew: {}, SideOld: {}}
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l.NewNo > 0 {
					a[p][SideNew][l.NewNo] = true
				}
				if l.OldNo > 0 {
					a[p][SideOld][l.OldNo] = true
				}
			}
		}
	}
	return a
}

// ok reports whether a comment can be anchored. A nil index (the diff was
// unavailable) accepts everything; a file-level comment needs the file in the diff.
func (a anchors) ok(path string, side Side, line int) bool {
	if a == nil {
		return true
	}
	f, in := a[path]
	if !in {
		return false
	}
	return line == 0 || f[side][line]
}

// PlanReview works out what SubmitReview would send for r.
func (s *Service) PlanReview(ctx context.Context, r Review, verdict Verdict, summary string) (SubmitPlan, error) {
	cur, ok := s.store.Get(r.ID)
	if !ok {
		return SubmitPlan{}, fmt.Errorf("review: unknown review %q", r.ID)
	}
	var idx anchors
	if files, err := s.Diff(ctx, cur); err == nil {
		idx = buildAnchors(files)
	}
	plan := SubmitPlan{Event: verdict.event(), Suggested: cur.OpenSuggestions()}
	var spill, notes []string
	for _, t := range cur.Threads {
		var refs []SentRef
		var texts []string
		for ci, c := range t.Comments {
			if !c.Sendable(Human) {
				continue
			}
			refs = append(refs, SentRef{Thread: t.ID, Index: ci})
			texts = append(texts, strings.TrimSpace(c.Text))
		}
		if len(refs) == 0 {
			continue
		}
		side := "RIGHT"
		if t.Side == SideOld {
			side = "LEFT"
		}
		if t.RemoteRoot != 0 { // a reply into an existing GitHub thread
			plan.Replies = append(plan.Replies, plannedReply{refs: refs, root: t.RemoteRoot,
				path: t.File, line: t.Line, side: side, texts: texts})
			continue
		}
		body := strings.Join(texts, "\n\n")
		if t.File == SummaryFile { // a review-level note: part of the summary, not "off-diff"
			notes = append(notes, body)
			plan.InBody += len(refs)
			for _, ref := range refs {
				plan.Inline = append(plan.Inline, plannedComment{ref: ref})
			}
			continue
		}
		if !idx.ok(t.File, t.Side, t.Line) {
			loc := t.File
			if t.Line > 0 {
				loc += ":" + strconv.Itoa(t.Line)
			}
			spill = append(spill, fmt.Sprintf("- **%s** — %s", loc, strings.ReplaceAll(body, "\n", "\n  ")))
			plan.InBody += len(refs)
			// Spilled comments still count as sent; remember them on the plan.
			for _, ref := range refs {
				plan.Inline = append(plan.Inline, plannedComment{ref: ref})
			}
			continue
		}
		in := ReviewCommentInput{Path: t.File, Body: body}
		if t.Line == 0 {
			in.SubjectType = "file"
		} else {
			in.Line, in.Side = t.Line, side
		}
		plan.Inline = append(plan.Inline, plannedComment{ref: refs[0], input: in})
		for _, ref := range refs[1:] { // follow-ups ride in the same inline comment
			plan.Inline = append(plan.Inline, plannedComment{ref: ref})
		}
	}
	plan.Body = strings.TrimSpace(summary)
	for _, n := range notes {
		if plan.Body != "" {
			plan.Body += "\n\n"
		}
		plan.Body += n
	}
	if len(spill) > 0 {
		if plan.Body != "" {
			plan.Body += "\n\n"
		}
		plan.Body += "Comments on lines outside the diff:\n" + strings.Join(spill, "\n")
	}
	return plan, nil
}

// carriers returns only the plan entries that produce an API comment.
func (p SubmitPlan) carriers() []ReviewCommentInput {
	var out []ReviewCommentInput
	for _, pc := range p.Inline {
		if pc.input.Path != "" {
			out = append(out, pc.input)
		}
	}
	return out
}

// SubmitReview sends ONE review as the authenticated user (F-029): the
// verdict, the summary, and every unsent comment of the human's own — and
// nothing an employee wrote that the human did not accept. It returns the
// plan that was sent and the review's URL.
func (s *Service) SubmitReview(ctx context.Context, r Review, verdict Verdict, summary string) (SubmitPlan, string, error) {
	if !s.HasGH() {
		return SubmitPlan{}, "", fmt.Errorf("review: gh unavailable — cannot submit a review")
	}
	if r.Target.PRNumber <= 0 {
		return SubmitPlan{}, "", fmt.Errorf("review: %s does not back a PR", r.ID)
	}
	mem, ok := s.ws.Member(r.Target.Member)
	if !ok {
		return SubmitPlan{}, "", fmt.Errorf("review: unknown member %q", r.Target.Member)
	}
	repo := s.remoteURL(mem.Path)
	if repo == "" {
		return SubmitPlan{}, "", fmt.Errorf("review: member %q has no origin remote", r.Target.Member)
	}
	plan, err := s.PlanReview(ctx, r, verdict, summary)
	if err != nil {
		return plan, "", err
	}
	if plan.Event == EventComment && plan.Body == "" && len(plan.Inline) == 0 && len(plan.Replies) == 0 {
		return plan, "", fmt.Errorf("review: nothing to send — add a comment or a summary first")
	}

	num := fmt.Sprint(r.Target.PRNumber)
	sha := r.Target.Head
	var meta PRMeta
	if sha == "" || plan.Event != EventComment {
		if meta, err = s.gh.PR(ctx, repo, num); err != nil {
			return plan, "", fmt.Errorf("review: fetch PR metadata: %w", err)
		}
		if sha == "" {
			sha = meta.HeadSHA
		}
	}
	if sha == "" {
		return plan, "", fmt.Errorf("review: cannot determine the PR head commit")
	}
	if plan.Event != EventComment {
		// GitHub refuses to approve or block your own PR; say so before sending.
		if login, lerr := s.gh.Login(ctx); lerr == nil && login != "" && strings.EqualFold(login, meta.Author) {
			return plan, "", fmt.Errorf("review: you cannot %s your own pull request — choose “comment”",
				strings.ToLower(strings.ReplaceAll(plan.Event, "_", " ")))
		}
	}

	url, err := s.gh.SubmitReview(ctx, repo, num, ReviewSubmission{
		CommitSHA: sha, Event: plan.Event, Body: plan.Body, Comments: plan.carriers(),
	})
	if err != nil {
		return plan, "", err
	}
	var sent []SentRef
	for _, pc := range plan.Inline {
		sent = append(sent, pc.ref)
	}
	if err := s.store.RecordSubmission(r.ID, string(verdict), plan.Body, url, sent); err != nil {
		return plan, url, err
	}
	// Replies into existing threads cannot ride in a review; send them now.
	var replied []SentRef
	for _, rp := range plan.Replies {
		line := rp.line
		if line == 0 {
			line = 1
		}
		for i, text := range rp.texts {
			if err := s.gh.PostReviewComment(ctx, repo, num, sha, rp.path, line, rp.side, text, rp.root); err != nil {
				_ = s.store.RecordSubmission(r.ID, string(verdict), plan.Body, url, replied)
				return plan, url, fmt.Errorf("review sent, but a reply failed: %w", err)
			}
			replied = append(replied, rp.refs[i])
		}
	}
	if len(replied) > 0 {
		_ = s.store.RecordSubmission(r.ID, string(verdict), plan.Body, url, replied)
	}
	return plan, url, nil
}
