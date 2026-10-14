package kit

import "strconv"

// Count renders "1 task" / "3 tasks" (F-093: no "task(s)" in the UI).
// The plural defaults to noun+"s"; pass it for irregular nouns
// ("1 child", "2 children").
func Count(n int, noun string, plural ...string) string {
	if n == 1 {
		return "1 " + noun
	}
	p := noun + "s"
	if len(plural) > 0 {
		p = plural[0]
	}
	return strconv.Itoa(n) + " " + p
}
