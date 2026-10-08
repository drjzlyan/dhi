package review

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Finding is one structured observation an employee makes in a review.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Side     string `json:"side"`
	Severity string `json:"severity"`
	Comment  string `json:"comment"`
}

// Findings is an employee's whole answer.
type Findings struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// SummaryFile is the pseudo-file that carries a review-level note.
const SummaryFile = "(review)"

var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*\n(.*?)\n?```")

// ParseFindings extracts the structured answer from an employee's reply:
// a fenced json block holding {"summary","findings":[…]} or a bare findings
// array. ok is false when nothing usable was found — callers then keep the
// raw reply as one review-level suggestion rather than dropping it.
func ParseFindings(text string) (Findings, bool) {
	var candidates []string
	for _, m := range fenceRe.FindAllStringSubmatch(text, -1) {
		candidates = append(candidates, m[1])
	}
	candidates = append(candidates, strings.TrimSpace(text))
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		var f Findings
		switch {
		case strings.HasPrefix(c, "{"):
			if json.Unmarshal([]byte(c), &f) != nil {
				continue
			}
		case strings.HasPrefix(c, "["):
			if json.Unmarshal([]byte(c), &f.Findings) != nil {
				continue
			}
		default:
			continue
		}
		f = f.clean()
		if f.Summary != "" || len(f.Findings) > 0 {
			return f, true
		}
		if isEmptyAnswer(c) { // {"findings": []} is a legitimate "clean"
			return f, true
		}
	}
	return Findings{}, false
}

// isEmptyAnswer reports a well-formed answer that says nothing found.
func isEmptyAnswer(c string) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(c), &raw) != nil {
		return false
	}
	_, ok := raw["findings"]
	return ok
}

func (f Findings) clean() Findings {
	out := Findings{Summary: strings.TrimSpace(f.Summary)}
	for _, x := range f.Findings {
		x.File = strings.TrimSpace(x.File)
		x.Comment = strings.TrimSpace(x.Comment)
		if x.File == "" || x.Comment == "" || x.Line < 0 {
			continue
		}
		x.Side = strings.ToLower(strings.TrimSpace(x.Side))
		if x.Side != string(SideOld) {
			x.Side = string(SideNew)
		}
		x.Severity = strings.ToLower(strings.TrimSpace(x.Severity))
		if len(x.Severity) > 12 {
			x.Severity = x.Severity[:12]
		}
		out.Findings = append(out.Findings, x)
	}
	return out
}

// AddSuggestions records an employee's answer as suggestions: one anchored
// thread per finding and, when there is a summary, one review-level thread.
// The raw fallback (used when the reply was not structured) becomes a single
// review-level suggestion so nothing the employee said is lost.
func (s *Store) AddSuggestions(id, agent string, f Findings, rawFallback string) (int, error) {
	if _, ok := s.Get(id); !ok {
		return 0, fmt.Errorf("review: unknown review %q", id)
	}
	n := 0
	add := func(file string, line int, side Side, severity, text string) error {
		_, err := s.AddThread(id, Thread{File: file, Line: line, Side: side, Comments: []Comment{
			{Author: agent, Text: text, Suggested: true, Severity: severity}}})
		if err == nil {
			n++
		}
		return err
	}
	if f.Summary != "" {
		if err := add(SummaryFile, 0, SideNew, "", f.Summary); err != nil {
			return n, err
		}
	}
	for _, x := range f.Findings {
		if err := add(x.File, x.Line, Side(x.Side), x.Severity, x.Comment); err != nil {
			return n, err
		}
	}
	if n == 0 && strings.TrimSpace(rawFallback) != "" {
		if err := add(SummaryFile, 0, SideNew, "", strings.TrimSpace(rawFallback)); err != nil {
			return n, err
		}
	}
	return n, nil
}
