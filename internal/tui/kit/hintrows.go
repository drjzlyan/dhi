package kit

import "strings"

// HintRows turns chrome-bar hints ("h/l lane", "[ ] sections",
// "enter/o jump") into (keys, description) help rows (F-054). The key
// part is the leading token, extended over following one-character
// tokens so paired keys like "[ ]" or "< >" stay together. Hints with no
// description become a key-only row.
func HintRows(hints ...string) [][2]string {
	out := make([][2]string, 0, len(hints))
	for _, h := range hints {
		toks := strings.Fields(h)
		if len(toks) == 0 {
			continue
		}
		n := 1
		for n < len(toks)-1 && len([]rune(toks[n])) == 1 {
			n++
		}
		out = append(out, [2]string{strings.Join(toks[:n], " "), strings.Join(toks[n:], " ")})
	}
	return out
}

// DedupeHelpRows drops rows whose keys repeat an earlier row's keys
// (compared without spaces and slashes, so "[ / ]" and "[ ]" match) —
// a surface's fixed rows and its live hints often name the same key.
func DedupeHelpRows(rows [][2]string) [][2]string {
	seen := map[string]bool{}
	out := make([][2]string, 0, len(rows))
	for _, r := range rows {
		k := strings.NewReplacer(" ", "", "/", "").Replace(r[0])
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

// ClickKey is the key a click on a hint presses: the first of "a/b"
// alternatives, "/" itself, and nothing for a range like "1-5" (F-115:
// "/ find" crashed the click; "1-5 views" pressed a key nobody handles).
func ClickKey(spec string) (string, bool) {
	if spec == "/" {
		return "/", true
	}
	f := strings.Fields(strings.ReplaceAll(spec, "/", " "))
	if len(f) == 0 {
		return "", false
	}
	k := f[0]
	if i := strings.LastIndex(k, "-"); i > 0 && i < len(k)-1 && isDigit(k[i-1]) && isDigit(k[i+1]) {
		return "", false
	}
	if k == "space" {
		k = " "
	}
	return k, true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
