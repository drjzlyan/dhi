package reviewer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// fixerTimeout bounds the synchronous task-store work in dispatchFixer.
const fixerTimeout = 30 * time.Second

// prCommentBody renders the consolidated markdown comment posted to the
// PR. Agent-authored comments carry explicit attribution.
func prCommentBody(r review.Review) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## DHI review `%s`\n\n%s → %s\n",
		r.ID, r.Target.Base, shortSHA(r.Target.Head))
	for _, t := range r.Threads {
		if len(t.Comments) == 0 {
			continue
		}
		loc := t.File
		if t.Line > 0 {
			loc += ":" + fmt.Sprint(t.Line)
		}
		state := ""
		if t.Resolved {
			state = " ✓ resolved"
		}
		fmt.Fprintf(&b, "\n**%s**%s\n", loc, state)
		for _, c := range t.Comments {
			if c.Author == busHuman() {
				fmt.Fprintf(&b, "- %s\n", c.Text)
				continue
			}
			fmt.Fprintf(&b, "- %s — _DHI agent @%s_\n", c.Text, c.Author)
		}
	}
	return b.String()
}

// postToPR publishes the review's threads to the PR as one consolidated
// comment via gh (PR-backed reviews only).
func (m *Model) postToPR() {
	r, ok := m.requireReview()
	if !ok {
		return
	}
	if r.Target.Kind != review.KindPR || r.Target.PRNumber <= 0 {
		m.opErr = "not a PR review — nothing to post to"
		return
	}
	body := prCommentBody(r)
	id := r.ID
	m.busy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		err := m.svc.PostComment(ctx, r, body)
		m.send(revEvent{kind: evPosted, id: id, err: errString(err), n: r.Target.PRNumber})
	}()
}

// dispatchFixer creates a task card for the review findings and binds it
// to the SAME worktree the review used, so a fixing agent works exactly
// where the findings apply.
func (m *Model) dispatchFixer() {
	r, ok := m.requireReview()
	if !ok {
		return
	}
	if m.taskStore == nil {
		m.opErr = "task store unavailable"
		return
	}
	slug := "fix-" + r.ID
	if _, exists := m.taskStore.Get(slug); exists {
		m.opErr = "task " + slug + " already exists"
		return
	}
	title := "Fix review findings: " + r.ID
	if err := m.taskStore.Create(slug, title, "", ""); err != nil {
		m.opErr = err.Error()
		return
	}
	if r.WorkRel != "" {
		cs := tasks.ChangeSet{
			Member: r.Target.Member,
			Branch: "review/" + r.ID,
			Path:   r.WorkRel,
		}
		if err := m.taskStore.RecordChangeSet(slug, cs); err != nil {
			m.opErr = err.Error()
			return
		}
	}
	if err := m.taskStore.SetStatus(slug, tasks.Active); err != nil {
		m.opErr = err.Error()
		return
	}
	if err := m.taskStore.BindThread(slug, r.Channel, 0); err != nil {
		m.opErr = err.Error()
		return
	}
	m.closeFormWithFlash("dispatched fixer task " + slug)
}

// handoffToEditor opens every changed file of the open review in the
// Editor and switches focus there.
func (m *Model) handoffToEditor() {
	r, ok := m.requireReview()
	if !ok {
		return
	}
	if m.openInEditor == nil {
		m.opErr = "editor handoff unavailable"
		return
	}
	mem, okMem := m.ws.Member(r.Target.Member)
	if !okMem {
		m.opErr = "member gone: " + r.Target.Member
		return
	}
	var paths []string
	for i := range m.files {
		paths = append(paths, filepath.Join(mem.Path, m.files[i].DisplayPath()))
	}
	if len(paths) == 0 {
		m.opErr = "no files to open"
		return
	}
	if !m.openInEditor(paths) {
		m.opErr = "editor refused the handoff"
	}
}

// requireReview fetches the open review, guarding nil services.
func (m *Model) requireReview() (review.Review, bool) {
	if m.svc == nil || m.ws == nil {
		return review.Review{}, false
	}
	return m.openReview()
}
