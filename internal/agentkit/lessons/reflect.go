package lessons

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Signal is why a turn is worth reflecting on (F-068): reflection costs
// tokens, so it runs only when something was learned the hard way.
type Signal string

// Signals.
const (
	SignalRetry      Signal = "retry"      // an attempt failed and was retried
	SignalVerify     Signal = "verify"     // DHI's check failed at least once
	SignalStuck      Signal = "stuck"      // the loop gave up and asked the human
	SignalCorrection Signal = "correction" // the human corrected the agent
)

// Evidence is what reflection sees: the turn's goal, how it went, and the
// concrete failures or corrections that carry the lesson.
type Evidence struct {
	Agent      string
	Goal       string
	Outcome    string // the final reply or the last error
	Signals    []Signal
	Failures   []string // failure briefs and verify tails, trimmed
	Correction string   // the human's correcting message, if any
	Known      []string // lessons the agent already holds (do not repeat)
}

// ReflectionPrompt is the self-review an agent CLI runs with no tools.
func ReflectionPrompt(ev Evidence) string {
	var b strings.Builder
	b.WriteString("You are reviewing your own just-finished work to learn from it. Do not use any tools.\n\n")
	fmt.Fprintf(&b, "Goal: %s\nOutcome: %s\nWhy this review: %s\n", clip(ev.Goal, 600), clip(ev.Outcome, 600), joinSignals(ev.Signals))
	if ev.Correction != "" {
		fmt.Fprintf(&b, "\nThe human corrected you:\n%s\n", clip(ev.Correction, 800))
	}
	for i, f := range ev.Failures {
		if i >= 4 {
			break
		}
		fmt.Fprintf(&b, "\nWhat went wrong (%d):\n%s\n", i+1, clip(f, 900))
	}
	if len(ev.Known) > 0 {
		b.WriteString("\nYou already know (do not repeat these):\n- " + strings.Join(ev.Known, "\n- ") + "\n")
	}
	b.WriteString(`
Extract at most 3 lessons that would have prevented the problem or that the correction teaches.
A lesson is ONE short imperative rule (under 200 characters) that will still be true next week,
specific to this codebase, tools or human — not generic advice ("write good code" is useless).
Use scope "user" only for the human's personal preferences (style, process, communication);
otherwise "private". If nothing durable was learned, return an empty list.

Reply with ONLY this JSON, no prose:
{"lessons":[{"text":"...","tags":["..."],"scope":"private","confidence":0.7}]}`)
	return b.String()
}

func joinSignals(ss []Signal) string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return strings.Join(out, ", ")
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

var jsonBlockRe = regexp.MustCompile(`(?s)\{.*\}`)

// ParseReflection reads the lessons out of a reflection reply. It
// tolerates code fences and surrounding prose; anything malformed yields
// an error (named in the thread, never a silent "learned nothing").
func ParseReflection(reply string) ([]Lesson, error) {
	raw := jsonBlockRe.FindString(reply)
	if raw == "" {
		return nil, fmt.Errorf("lessons: reflection reply has no JSON object")
	}
	var doc struct {
		Lessons []struct {
			Text       string   `json:"text"`
			Tags       []string `json:"tags"`
			Scope      string   `json:"scope"`
			Confidence float64  `json:"confidence"`
		} `json:"lessons"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("lessons: reflection reply: %w", err)
	}
	var out []Lesson
	for _, l := range doc.Lessons {
		text := strings.TrimSpace(l.Text)
		if text == "" || len([]rune(text)) > 300 {
			continue
		}
		scope := ScopePrivate
		if l.Scope == ScopeUser {
			scope = ScopeUser
		}
		conf := l.Confidence
		if conf <= 0 || conf > 1 {
			conf = 0.6
		}
		out = append(out, Lesson{Text: text, Tags: l.Tags, Scope: scope, Confidence: conf, Source: SourceReflection})
		if len(out) == 3 {
			break
		}
	}
	return out, nil
}

// correctionRe spots a human reply that corrects the agent: a reversal,
// a "don't", an "instead", a plain "no"/"wrong".
var correctionRe = regexp.MustCompile(`(?i)^(no\b|nope\b|wrong\b|that's not|that is not|not quite)|\b(don't|do not|instead|stop doing|never do|why did you|you should have|actually,|that's wrong|please don't)\b`)

// IsCorrection reports whether a human message corrects the agent's
// previous work (the cheapest strong learning signal there is).
func IsCorrection(text string) bool {
	return correctionRe.MatchString(strings.TrimSpace(text))
}
