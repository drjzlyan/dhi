package reviewer

import (
	"context"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// mentionedAgent returns the first rostered @mention in text.
func (m *Model) mentionedAgent(text string) string {
	if m.crew == nil {
		return ""
	}
	want := map[string]bool{}
	for _, id := range m.crew.AgentIDs() {
		want[id] = true
	}
	for _, id := range bus.Mentions(text) {
		if want[id] {
			return id
		}
	}
	return ""
}

// invite posts the comment into the review's bus conversation and
// dispatches a turn to the mentioned agent; its reply lands back on the
// bus in the same thread and mirrors into the local review threads.
func (m *Model) invite(threadID int64, text string) {
	if m.bus == nil || m.crew == nil || m.svc == nil {
		m.opErr = "agent crew unavailable — no bus or runtime"
		return
	}
	r, ok := m.openReview()
	if !ok {
		return
	}
	st := m.svc.Store()
	cur, _ := st.Get(r.ID)
	var t *review.Thread
	for i := range cur.Threads {
		if cur.Threads[i].ID == threadID {
			t = &cur.Threads[i]
			break
		}
	}
	if t == nil {
		return
	}

	ctx := context.Background()
	root := t.BusThread
	if root == 0 {
		head := fmt.Sprintf("review thread — %s:%d (%s...%s)",
			t.File, t.Line, r.Target.Base, shortSHA(r.Target.Head))
		posted, err := m.bus.Post(bus.Message{
			Channel: r.Channel, Author: busHuman(),
			Text: head + "\n" + text,
		})
		if err != nil {
			m.opErr = err.Error()
			return
		}
		root = posted.ID
		if err := st.SetBusThread(r.ID, threadID, root); err != nil {
			m.opErr = err.Error()
			return
		}
	} else {
		if _, err := m.bus.Post(bus.Message{
			Channel: r.Channel, Thread: root, Author: busHuman(), Text: text,
		}); err != nil {
			m.opErr = err.Error()
			return
		}
	}
	// Handle dispatches every rostered mention; replies arrive via evBus.
	m.crew.Handle(ctx, bus.Message{Channel: r.Channel, Thread: root,
		Author: busHuman(), Text: text})
}

// mirrorBus folds an inbound bus message into the open review's threads.
// Own messages are ignored; agent replies attach to the matching thread
// (or open a new file-level one for whole-review responses).
func (m *Model) mirrorBus(msg bus.Message) {
	if m.svc == nil || msg.Author == busHuman() || msg.Author == "" {
		return
	}
	r, ok := m.openReview()
	if !ok || msg.Channel != r.Channel {
		return
	}
	st := m.svc.Store()
	cur, _ := st.Get(r.ID)
	root := threadRoot(msg)
	for _, t := range cur.Threads {
		if t.BusThread == root {
			if err := st.AppendComment(r.ID, t.ID, review.Comment{
				Author: msg.Author, Text: msg.Text,
			}); err != nil {
				m.opErr = err.Error()
			}
			return
		}
	}
	// Unmatched: a whole-review response becomes its own resolved=false
	// file-level thread authored by the agent.
	id, err := st.AddThread(r.ID, review.Thread{
		File: "(review)", Side: review.SideNew, BusThread: root,
		Comments: []review.Comment{{Author: msg.Author, Text: msg.Text}},
	})
	if err != nil {
		m.opErr = err.Error()
		return
	}
	_ = id
}

// requestAgentReview posts the diff to the crew channel and asks the
// named agent for a full review of the selected files.
func (m *Model) requestAgentReview(agent string, files []string) {
	if !m.canAgent() {
		m.opErr = "agent crew unavailable — no bus or runtime"
		return
	}
	r, ok := m.openReview()
	if !ok {
		return
	}
	m.busy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		patch, err := m.svc.Patch(ctx, r)
		ev := revEvent{kind: evAgentDone}
		if err != nil {
			ev.err = err.Error()
			m.send(ev)
			return
		}
		if len(patch) > maxPromptPatch {
			patch = patch[:maxPromptPatch] + "\n… (truncated)"
		}
		scope := "all files"
		if len(files) > 0 && !(len(files) == 1 && files[0] == ".") {
			scope = strings.Join(files, ", ")
		}
		text := fmt.Sprintf("@%s please review these changes (%s → %s, %s).\n"+
			"Answer with concrete findings per file; say LGTM when clean.\n"+
			"```diff\n%s\n```",
			agent, r.Target.Base, shortSHA(r.Target.Head), scope, patch)
		posted, perr := m.bus.Post(bus.Message{
			Channel: r.Channel, Author: busHuman(), Text: text,
		})
		if perr != nil {
			ev.err = perr.Error()
			m.send(ev)
			return
		}
		m.crew.Handle(ctx, posted)
		m.send(ev)
	}()
}

func (m *Model) canAgent() bool {
	return m.bus != nil && m.crew != nil && m.svc != nil
}

// threadRoot applies the bus convention: replies carry the root id,
// roots carry their own.
func threadRoot(msg bus.Message) int64 {
	if msg.Thread != 0 {
		return msg.Thread
	}
	return msg.ID
}

// agentBanner is the shared degradation notice for agent features.
func (m *Model) agentBanner() string {
	if m.canAgent() {
		return ""
	}
	return theme.TextDim().Render("(agents offline — no bus/runtime)")
}
