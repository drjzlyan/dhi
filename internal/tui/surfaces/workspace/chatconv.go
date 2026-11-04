package workspace

import (
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
)

// F-134: /fork and /new in a DM composer. A DM conversation is a thread
// (its episode is keyed by the thread root), so a fork is a new thread
// whose episode starts as a copy of the open one, and "new conversation"
// is leaving the open thread so the next top-level message roots a fresh one.

// forkAuthor signs the fork's thread root (the runtime's notice author).
const forkAuthor = "dhi"

// parseConvCmd reads "/fork" or "/new [message]" typed as a whole
// message; rest is the text after /new.
func parseConvCmd(text string) (cmd, rest string, ok bool) {
	text = strings.TrimSpace(text)
	head, tail, _ := strings.Cut(text, " ")
	switch strings.ToLower(head) {
	case "/fork":
		if strings.TrimSpace(tail) != "" {
			return "", "", false
		}
		return "fork", "", true
	case "/new":
		return "new", strings.TrimSpace(tail), true
	}
	return "", "", false
}

// conversationCommand handles /fork and /new; false leaves the text to post.
func (p *chatPane) conversationCommand(text string) bool {
	cmd, rest, ok := parseConvCmd(text)
	if !ok {
		return false
	}
	p.flash, p.note = "", ""
	ch := p.channelName()
	if !bus.IsDM(ch) {
		p.flash = "/" + cmd + " works in a direct message with an agent"
		return true
	}
	agent := bus.ChannelLabel(ch)
	if cmd == "new" {
		p.threadID, p.profileID, p.cursor = 0, "", 0
		p.resubscribe()
		if rest != "" {
			p.post(rest)
			return true
		}
		p.note = "next message starts a new conversation with " + agent
		return true
	}
	p.fork(ch, agent)
	return true
}

// fork starts a new thread from the open one: a root message (the
// thread's identity) and an episode copied from the source's. The source
// thread and episode are not touched.
func (p *chatPane) fork(ch, agent string) {
	if p.meter == nil || p.meter.episodes == nil || p.bus == nil {
		p.flash = "/fork needs a workspace"
		return
	}
	if p.threadID == 0 {
		p.flash = "open the conversation to fork first (t on a message), then /fork"
		return
	}
	srcKey := episode.Key("", ch, p.threadID)
	if _, ok, err := p.meter.episodes.Get(agent, srcKey); err != nil || !ok {
		p.flash = "no conversation to fork yet: " + agent + " has not replied in this thread"
		return
	}
	root, err := p.bus.Post(bus.Message{
		Channel: ch, Author: forkAuthor,
		Text: "forked from an earlier conversation with " + agent,
	})
	if err != nil {
		p.flash = "fork failed: " + err.Error()
		return
	}
	if _, err := p.meter.episodes.Fork(agent, srcKey, episode.Key("", ch, root.ID), time.Now().UTC()); err != nil {
		p.flash = "fork failed: " + err.Error()
		return
	}
	p.threadID, p.profileID, p.cursor = root.ID, "", 0
	p.markThreadRead(root.ID)
	p.resubscribe()
	p.meter.forget()
	p.note = "forked: " + agent + " continues from the same context here; the original is untouched"
}
