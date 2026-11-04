package workspace

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
)

// F-130: /model in a DM sets the model the agent uses for this
// conversation. The override lives on the episode (the runtime reads it
// each turn); the header chip shows it.

// parseModelCmd reads "/model", "/model <name>" or "/model clear" typed
// as a whole message. Anything else is not the command.
func parseModelCmd(text string) (arg string, ok bool) {
	f := strings.Fields(text)
	if len(f) == 0 || strings.ToLower(f[0]) != "/model" || len(f) > 2 {
		return "", false
	}
	if len(f) == 2 {
		arg = f[1]
	}
	return arg, true
}

// modelCommand handles a /model message; false leaves it to post as text.
func (p *chatPane) modelCommand(text string) bool {
	arg, ok := parseModelCmd(text)
	if !ok {
		return false
	}
	p.flash, p.note = "", ""
	ch := p.channelName()
	if !bus.IsDM(ch) {
		p.flash = "/model works in a direct message with an agent"
		return true
	}
	if p.meter == nil || p.meter.episodes == nil {
		p.flash = "/model needs a workspace"
		return true
	}
	agent := bus.ChannelLabel(ch)
	ep, found := meterEpisode(p.meter.episodes, ch, p.threadID)
	if !found {
		// A conversation exists once the agent has a turn in it; refuse
		// by name rather than park an override on a thread that is not there.
		p.flash = "no conversation yet: send " + agent + " a message first, then /model"
		return true
	}
	switch {
	case arg == "":
		if ep.Model == "" {
			p.note = agent + " uses its configured model here; /model <name> overrides it for this conversation"
		} else {
			p.note = agent + " uses " + ep.Model + " here; /model clear returns to its configured model"
		}
	case strings.EqualFold(arg, "clear"):
		p.setModel(agent, ep, "", agent+" is back on its configured model")
	case strings.HasPrefix(arg, "-"):
		p.flash = "a model name cannot start with '-'"
	default:
		p.setModel(agent, ep, arg, agent+" will use "+arg+" in this conversation")
	}
	return true
}

func (p *chatPane) setModel(agent string, ep episode.Episode, model, ok string) {
	if err := p.meter.episodes.SetModel(agent, ep.Key, model); err != nil {
		p.flash = "could not save the model: " + err.Error()
		return
	}
	p.meter.forget()
	p.note = ok
}
