package runtime

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
)

// teammateVoice is the system-prompt note that keeps chat human (F-072):
// the final reply is what teammates read, so it carries no tool logs.
const teammateVoice = "Your final reply is posted to team chat (or the task's thread) for people to read. " +
	"Write it like a teammate: short plain prose, what you did and what is left, no tool logs, " +
	"no command transcripts, no internal reasoning. Put code, diffs and commands in fenced ``` blocks " +
	"with a language tag. Use channel_post only for a brief update a person needs before you finish."

// alert posts a runtime event the human must act on (F-072): shown in
// chat as a system line under the agent's name, counted as attention,
// never replayed to the agent as something it said.
func (r *Runtime) alert(trigger bus.Message, author, text string) {
	_, _ = r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel, Thread: trigger.Thread,
		Author: author, Text: text, Kind: bus.KindAlert,
	})
}

// errAlerted marks a turn error whose cause was already told to the
// human, so Turn does not post a second, vaguer alert.
var errAlerted = errors.New("alerted")

// humanFailure is the one-line, plain-language reason a turn failed.
func humanFailure(fail clirun.Failure, c *clirun.CLI) string {
	name := "the CLI"
	if c != nil && c.Name != "" {
		name = c.Name
	}
	switch fail {
	case clirun.FailAuth:
		return "I couldn't finish: " + authHint(c) + "."
	case clirun.FailRateLimit:
		return "I couldn't finish: the model provider is rate-limiting " + name + ". Ask again in a little while."
	case clirun.FailOverflow:
		return "I couldn't finish: the conversation got too long for " + name + ", even compacted. Start a fresh thread with the essentials."
	case clirun.FailTimeout:
		return "I couldn't finish: the turn ran past its time limit."
	case clirun.FailStall:
		return "I couldn't finish: " + name + " stopped responding and was stopped."
	case clirun.FailNoFinal:
		return "I couldn't finish: " + name + " ended without an answer."
	}
	return "I couldn't finish: " + name + " exited with an error. The trace has the details."
}

// toolEcho matches lines a CLI sometimes leaks into its final text: tool
// call echoes and command transcripts, not prose a person wrote.
var toolEcho = regexp.MustCompile(`^\s*(?:(?:tool|command|bash|file_change)\s*:|<(?:/?)(?:tool_use|function_calls|invoke|parameter)[ >])`)

// sanitizeReply drops leaked tool-echo lines from a final reply, outside
// code fences only. It never empties a reply: if nothing but echoes
// remain, the original text is kept.
func sanitizeReply(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	fenced := false
	for _, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
		}
		if !fenced && toolEcho.MatchString(l) {
			continue
		}
		out = append(out, l)
	}
	cleaned := strings.TrimSpace(strings.Join(out, "\n"))
	if cleaned == "" {
		return s
	}
	return cleaned
}

// turnRefused is the alert for a turn refused before it started.
func turnRefused(err error) string {
	return fmt.Sprintf("I couldn't start: %v", err)
}

// projectBlock is the project brief (F-080), read fresh each turn so a
// Settings edit applies on the next one. A malformed brief is left out
// here and named by doctor and Settings → PROJECT.
func (r *Runtime) projectBlock() string {
	b, ok, err := brief.Load(r.cfg.WS.Root)
	if err != nil || !ok {
		return ""
	}
	return b.Render()
}
