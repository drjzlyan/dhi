package tasks

import (
	"fmt"
	"strings"
	"time"
)

// Comment is one durable note on a card (F-037). Agents and the human
// write comments; they are the card's own record, separate from the
// bound chat thread (which stays the live conversation).
type Comment struct {
	Author string    `toml:"author"`
	Text   string    `toml:"text"`
	At     time.Time `toml:"at"`
}

// Activity kinds.
const (
	ActStatus   = "status"
	ActAssignee = "assignee"
	ActPriority = "priority"
	ActLabels   = "labels"
	ActRun      = "run"
	ActComment  = "comment"
)

// Activity is one entry of a card's audit trail (F-037): what changed,
// from what to what, by whom ("" = not attributed), and when.
type Activity struct {
	Kind  string    `toml:"kind"`
	From  string    `toml:"from,omitempty"`
	To    string    `toml:"to,omitempty"`
	Actor string    `toml:"actor,omitempty"`
	At    time.Time `toml:"at"`
}

const (
	maxCommentLen = 4000
	maxActivity   = 200 // oldest entries are trimmed past this
)

func validActivityKind(k string) bool {
	switch k {
	case ActStatus, ActAssignee, ActPriority, ActLabels, ActRun, ActComment:
		return true
	}
	return false
}

// record appends an activity entry, trimming the oldest past the cap.
func (t *Task) record(kind, from, to, actor string, at time.Time) {
	t.Activity = append(t.Activity, Activity{Kind: kind, From: from, To: to, Actor: actor, At: at})
	if over := len(t.Activity) - maxActivity; over > 0 {
		t.Activity = append([]Activity(nil), t.Activity[over:]...)
	}
}

// AddComment appends a comment by author (an agent id or "you").
func (s *Store) AddComment(slug, author, text string) error {
	author = strings.TrimSpace(author)
	text = strings.TrimSpace(text)
	if !validMember(author) {
		return fmt.Errorf("tasks: bad comment author %q", author)
	}
	if text == "" {
		return fmt.Errorf("tasks: comment text required")
	}
	if len(text) > maxCommentLen {
		return fmt.Errorf("tasks: comment is %d bytes, max %d", len(text), maxCommentLen)
	}
	return s.mutate(slug, func(t *Task) {
		at := s.now()
		t.Comments = append(t.Comments, Comment{Author: author, Text: text, At: at})
		t.record(ActComment, "", firstLine(text), author, at)
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:59]) + "…"
	}
	return s
}

// SetStatusAs is SetStatus attributing the move to actor.
func (s *Store) SetStatusAs(slug string, st Status, actor string) error {
	if !ValidStatus(st) {
		return fmt.Errorf("tasks: bad status %q", st)
	}
	return s.mutate(slug, func(t *Task) {
		if t.Status != st {
			t.record(ActStatus, string(t.Status), string(st), actor, s.now())
		}
		t.Status = st
	})
}

// AssignAs is Assign attributing the change to actor.
func (s *Store) AssignAs(slug, who, actor string) error {
	if who != "" && !validMember(who) {
		return fmt.Errorf("tasks: bad assignee %q", who)
	}
	return s.mutate(slug, func(t *Task) {
		if t.Assignee != who {
			t.record(ActAssignee, t.Assignee, who, actor, s.now())
		}
		t.Assignee = who
	})
}
