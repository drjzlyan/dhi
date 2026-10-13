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
