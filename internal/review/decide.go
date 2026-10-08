package review

import (
	"fmt"
	"strings"
)

// SentRef addresses one comment that went out in a submitted review.
type SentRef struct {
	Thread int64
	Index  int
}

func (s *Store) commentAt(id string, threadID int64, idx int) (Comment, error) {
	s.mu.RLock()
	r, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return Comment{}, fmt.Errorf("review: unknown review %q", id)
	}
	for _, t := range r.Threads {
		if t.ID != threadID {
			continue
		}
		if idx < 0 || idx >= len(t.Comments) {
			return Comment{}, fmt.Errorf("review: %s: comment %d missing in thread %d", id, idx, threadID)
		}
		return t.Comments[idx], nil
	}
	return Comment{}, fmt.Errorf("review: %s: unknown thread %d", id, threadID)
}

// AcceptSuggestion turns an employee's suggestion into the human's own
// draft comment (F-049): the text may be edited on the way (text != "").
// Only an undecided suggestion can be accepted.
func (s *Store) AcceptSuggestion(id string, threadID int64, idx int, human, text string) error {
	c, err := s.commentAt(id, threadID, idx)
	if err != nil {
		return err
	}
	if !c.Suggested || c.Dismissed {
		return fmt.Errorf("review: %s: comment %d in thread %d is not an open suggestion", id, idx, threadID)
	}
	return s.mutate(id, func(r *Review) {
		for i := range r.Threads {
			if r.Threads[i].ID != threadID {
				continue
			}
			cm := &r.Threads[i].Comments[idx]
			if t := strings.TrimSpace(text); t != "" {
				cm.Text = t
			}
			cm.Author, cm.Suggested, cm.Pending, cm.Severity = human, false, true, ""
			return
		}
	})
}

// DismissSuggestion hides a suggestion without sending it; it stays on the
// card so the decision is auditable.
func (s *Store) DismissSuggestion(id string, threadID int64, idx int) error {
	c, err := s.commentAt(id, threadID, idx)
	if err != nil {
		return err
	}
	if !c.Suggested {
		return fmt.Errorf("review: %s: comment %d in thread %d is not a suggestion", id, idx, threadID)
	}
	return s.mutate(id, func(r *Review) {
		for i := range r.Threads {
			if r.Threads[i].ID == threadID {
				r.Threads[i].Comments[idx].Dismissed = true
				return
			}
		}
	})
}

// RecordSubmission marks the sent comments posted and stores what the
// review said. A later submission therefore sends only what is new.
func (s *Store) RecordSubmission(id, verdict, summary, url string, sent []SentRef) error {
	if _, ok := s.Get(id); !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	return s.mutate(id, func(r *Review) {
		for _, ref := range sent {
			for i := range r.Threads {
				if r.Threads[i].ID == ref.Thread && ref.Index < len(r.Threads[i].Comments) {
					c := &r.Threads[i].Comments[ref.Index]
					c.Posted, c.Pending = true, false
				}
			}
		}
		r.Status, r.Posted = Submitted, true
		r.Verdict, r.Summary = verdict, summary
		if url != "" {
			r.PRURL = url
		}
	})
}

// OpenSuggestions counts suggestions still awaiting a decision.
func (r Review) OpenSuggestions() int {
	n := 0
	for _, t := range r.Threads {
		for _, c := range t.Comments {
			if c.Suggested && !c.Dismissed {
				n++
			}
		}
	}
	return n
}
