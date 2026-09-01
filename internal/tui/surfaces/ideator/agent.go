package ideator

import (
	"context"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ideation"
)

// busHuman is the author name for messages the user posts.
const busHuman = bus.Human

// artifactRefRe matches artifact vpaths inside agent chatter, e.g.
// ".dhi/sessions/<session>/<rel-path>".
var artifactRefRe = regexp.MustCompile(`\.dhi/sessions/[a-z0-9][a-z0-9._-]*/[A-Za-z0-9._/-]+`)

// dispatchRevision routes rejection notes back to the authoring agent:
// a revision request is posted to the session channel (mentioning the
// author when known) and dispatched as an agent turn. Without a crew the
// request still lands in the channel so nothing is lost.
func (m *Model) dispatchRevision(rel, notes string) {
	sess, ok := m.openSession()
	if !ok || m.bus == nil {
		return
	}
	vp := ideation.VPathFor(sess, rel)
	author, _ := m.store.Artifact(sess.ID, rel)
	text := "please revise `" + vp + "`: " + notes
	if author.Author != "" {
		text = "@" + author.Author + " " + text
	}
	posted, err := m.bus.Post(bus.Message{Channel: sess.Channel, Author: busHuman, Text: text})
	if err != nil {
		m.opErr = "post revision request: " + err.Error()
		return
	}
	m.requestTurn(posted)
}

// requestTurn dispatches one bus message to the crew (no-op without a
// runtime). Synchronous like the reviewer's invite: the runtime's
// Handle returns immediately after fanning out per-agent goroutines.
func (m *Model) requestTurn(msg bus.Message) {
	if m.crew == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	m.crew.Handle(ctx, msg)
}

// mirrorBus folds inbound session-channel traffic into the store: agent
// messages that reference artifact paths claim authorship of matching
// unclaimed artifacts, so rejection routing knows who to ask.
func (m *Model) mirrorBus(msg bus.Message) {
	if m.bus == nil || m.store == nil || msg.Author == busHuman || strings.TrimSpace(msg.Text) == "" {
		return
	}
	sess, ok := m.openSession()
	if !ok || msg.Channel != sess.Channel {
		return
	}
	for _, ref := range artifactRefRe.FindAllString(msg.Text, -1) {
		rel := strings.TrimPrefix(ref, ideation.ReservedPrefix+sess.ID+"/")
		if rel == ref {
			continue // some other session's artifact
		}
		if a, exists := m.store.Artifact(sess.ID, rel); exists && a.Author == "" {
			if err := m.store.ClaimAuthor(sess.ID, rel, msg.Author); err == nil {
				_ = m.store.Scan(sess.ID)
			}
		}
	}
}
