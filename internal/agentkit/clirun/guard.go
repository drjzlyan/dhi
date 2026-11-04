package clirun

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Repeated-call detection (G-C4). An agent stuck retrying the same tool
// call burns budget and says nothing; the runtime stops such a run by
// name instead of waiting for the turn limit.
const (
	// LoopRun is how many identical calls in a row stop a run.
	LoopRun = 8
	// loopWindow / loopWindowHits: the same call this many times among
	// the last loopWindow calls also stops it, when the whole window is
	// at most two distinct calls (an A/B ping-pong). An edit-then-test
	// loop repeats the test but its edits differ, so it runs on.
	loopWindow     = 24
	loopWindowHits = 12
	loopDistinct   = 2
)

// ToolCall is the identity of one tool call: the tool name plus its
// input in a canonical form (object keys sorted by encoding/json,
// whitespace in strings collapsed), so reordering or re-spacing the
// same arguments is still the same call.
func ToolCall(name string, input any) string {
	if s, ok := input.(string); ok {
		input = strings.Join(strings.Fields(s), " ")
	}
	b, err := json.Marshal(input)
	if err != nil {
		return name
	}
	return name + " " + string(b)
}

// LoopGuard watches one run's tool calls. The zero value uses the
// package limits.
type LoopGuard struct {
	run, hits int // 0 = the package defaults
	recent    []string
	last      string
	streak    int
}

// Observe records one EventCommand's Call and returns a reason once the
// run is looping, "" otherwise. Calls without an identity are ignored.
func (g *LoopGuard) Observe(call string) string {
	if call == "" {
		return ""
	}
	run, hits := g.run, g.hits
	if run == 0 {
		run = LoopRun
	}
	if hits == 0 {
		hits = loopWindowHits
	}
	if call == g.last {
		g.streak++
	} else {
		g.last, g.streak = call, 1
	}
	g.recent = append(g.recent, call)
	if len(g.recent) > loopWindow {
		g.recent = g.recent[1:]
	}
	if g.streak >= run {
		return fmt.Sprintf("the same tool call %d times in a row: %s", g.streak, truncate(call, 120))
	}
	n, distinct := 0, map[string]bool{}
	for _, c := range g.recent {
		distinct[c] = true
		if c == call {
			n++
		}
	}
	if n >= hits && len(distinct) <= loopDistinct {
		return fmt.Sprintf("the same tool call %d times in its last %d calls: %s", n, len(g.recent), truncate(call, 120))
	}
	return ""
}
