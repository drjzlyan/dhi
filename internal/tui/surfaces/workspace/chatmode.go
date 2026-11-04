package workspace

import (
	"context"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/behavior"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
)

// F-144: /plan, /review and /ask in a DM composer start a read-only turn.
// /plan ends in an approval card; only an approved plan starts the write turn.

// readOnlyRunner is the runtime's read-only turn.
type readOnlyRunner interface {
	ReadOnlyTurn(ctx context.Context, agent string, trigger bus.Message, mode, request string) error
}

// parseModeCmd reads "/plan <request>", "/review <what>" or "/ask <question>"
// typed as a whole message. A bare command is still the command: it is
// answered with its usage rather than posted as text.
func parseModeCmd(text string) (mode, request string, ok bool) {
	head, tail, _ := strings.Cut(strings.TrimSpace(text), " ")
	switch strings.ToLower(head) {
	case "/plan":
		mode = behavior.ModePlan
	case "/review":
		mode = behavior.ModeReview
	case "/ask":
		mode = behavior.ModeAsk
	default:
		return "", "", false
	}
	return mode, strings.TrimSpace(tail), true
}

// modeCommand handles a mode message; false leaves it to post as text.
func (p *chatPane) modeCommand(text string) bool {
	mode, request, ok := parseModeCmd(text)
	if !ok {
		return false
	}
	p.flash, p.note = "", ""
	ch := p.channelName()
	if !bus.IsDM(ch) {
		p.flash = "/" + mode + " works in a direct message with an agent"
		return true
	}
	if request == "" {
		p.flash = "/" + mode + " needs text: /" + mode + " <what you want>"
		return true
	}
	run, ok := p.rt.(readOnlyRunner)
	if !ok || p.bus == nil {
		p.flash = "/" + mode + " needs the crew running"
		return true
	}
	agent := bus.ChannelLabel(ch)
	posted, err := p.bus.Post(bus.Message{
		Channel: ch, Thread: p.threadID, Author: bus.Human,
		Text: "/" + mode + " " + request,
	})
	if err != nil {
		p.flash = "post failed: " + err.Error()
		return true
	}
	if p.threadID != 0 {
		p.markThreadRead(p.threadID)
	} else {
		p.markChannelRead(ch)
	}
	// The runtime posts the outcome (or why not) into the thread.
	go func() { _ = run.ReadOnlyTurn(context.Background(), agent, posted, mode, request) }()
	if mode == behavior.ModePlan {
		p.note = agent + " is planning, read-only; the plan comes back as an approval card (alt+y approve, alt+n reject)"
	} else {
		p.note = agent + " is working read-only (" + mode + ")"
	}
	select {
	case p.events <- struct{}{}:
	default:
	}
	return true
}
