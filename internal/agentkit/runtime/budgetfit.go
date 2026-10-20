package runtime

import (
	"fmt"
	"strings"
)

// F-078: one token budget for the whole prompt. Every block — persona,
// rules, task, history, lessons, memory, knowledge — is a section with a
// priority and a floor; when the assembled prompt is over budget the
// lowest-priority sections shrink first, down to their floors, so the
// trigger and the task always survive and nothing grows without bound.

// section is one prompt block.
type section struct {
	name  string
	text  string
	prio  int  // 0 = shrink last; higher shrinks first
	floor int  // runes a section keeps however tight the budget
	tail  bool // keep the end (history: newest last) instead of head+tail
}

// runesPerToken estimates prompt size without a tokenizer: English prose
// and code average about four runes per token.
const runesPerToken = 4

// compactionFactor scales the budget per compaction level: a retry after
// an overflow gets a smaller prompt each time.
var compactionFactor = [...]float64{1, 0.55, 0.3}

// fitSections shrinks sections, lowest priority first, until the total
// fits budgetRunes; it returns the sections in their original order.
func fitSections(secs []section, budgetRunes int) []section {
	total := 0
	for _, s := range secs {
		total += runeLen(s.text)
	}
	excess := total - budgetRunes
	if excess <= 0 {
		return secs
	}
	maxPrio := 0
	for _, s := range secs {
		maxPrio = max(maxPrio, s.prio)
	}
	for p := maxPrio; p >= 0 && excess > 0; p-- {
		for i := range secs {
			if secs[i].prio != p || excess <= 0 {
				continue
			}
			n := runeLen(secs[i].text)
			keep := max(secs[i].floor, n-excess)
			if keep >= n {
				continue
			}
			secs[i].text = shrinkRunes(secs[i].text, keep, secs[i].tail)
			excess -= n - runeLen(secs[i].text)
		}
	}
	return secs
}

// shrinkRunes cuts s to about n runes with a visible marker: the end for
// tail sections, else the head and the end (a notes file's newest lines
// matter as much as its first).
func shrinkRunes(s string, n int, tail bool) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	marker := fmt.Sprintf(" …[%d chars trimmed for the prompt budget]… ", len(r)-n)
	if n <= len([]rune(marker))+8 {
		if tail {
			return string(r[len(r)-n:])
		}
		return string(r[:n])
	}
	room := n - len([]rune(marker))
	if tail {
		return marker + string(r[len(r)-room:])
	}
	head := room * 3 / 5
	return string(r[:head]) + marker + string(r[len(r)-(room-head):])
}

func runeLen(s string) int { return len([]rune(s)) }

func joinSections(secs []section) string {
	var b strings.Builder
	for _, s := range secs {
		b.WriteString(s.text)
	}
	return b.String()
}

// sectionTags names the tag a section is wrapped in when it differs from
// the section's own name (F-099). Tags make each block a distinct input
// the model can refer to; sections sharing a tag (the history head and
// its messages) join inside one.
var sectionTags = map[string]string{
	"grounding":    "operating_rules",
	"trigger":      "request",
	"history-head": "history",
	"forge":        "your_tools",
	"compacted":    "note",
	"voice":        "who_you_work_for",
}

func tagOf(name string) string {
	if t, ok := sectionTags[name]; ok {
		return t
	}
	return strings.ReplaceAll(name, "-", "_")
}

// joinTagged joins fitted sections, each wrapped in its tag. Tags are
// added after fitSections, never inside trimmable text, so a trimmed
// section keeps both tags; empty sections get none. Order is the
// sections' order, so the stable prefix the prompt cache keys on stays
// stable. A lone section (a bare request with no task or history) stays
// untagged: one block needs no labels.
func joinTagged(secs []section) string {
	tags := map[string]bool{}
	for _, s := range secs {
		if strings.TrimSpace(s.text) != "" {
			tags[tagOf(s.name)] = true
		}
	}
	if len(tags) <= 1 {
		return strings.TrimSpace(joinSections(secs))
	}
	var b strings.Builder
	open := ""
	for _, s := range secs {
		text := strings.Trim(s.text, "\n")
		if strings.TrimSpace(text) == "" {
			continue
		}
		tag := tagOf(s.name)
		if tag == open {
			b.WriteString("\n")
		} else {
			if open != "" {
				b.WriteString("\n</" + open + ">\n\n")
			}
			b.WriteString("<" + tag + ">\n")
			open = tag
		}
		b.WriteString(text)
	}
	if open != "" {
		b.WriteString("\n</" + open + ">")
	}
	return b.String()
}

// tagOverhead is the runes joinTagged adds around secs at most, so the
// budget fit can leave room for them.
func tagOverhead(secs []section) int {
	n := 0
	for _, s := range secs {
		if strings.TrimSpace(s.text) != "" {
			t := tagOf(s.name)
			n += 2*runeLen(t) + 9 // <t>\n … \n</t>\n\n
		}
	}
	return n
}
