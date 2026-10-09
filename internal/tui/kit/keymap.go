package kit

import "strings"

// Key remapping (F-058). The user's [keys] table maps the key they press
// (physical) to the key DHI acts on (logical): "ctrl+k" = "ctrl+p" opens
// the palette with ctrl+k. The shell translates input once; everything
// that SHOWS a key (hint bars, help, the statusline, the palette) renders
// it through DisplayKey, so the screen always names the key to press.
var (
	toLogical  = map[string]string{}
	toPhysical = map[string]string{}
)

// SetKeyMap installs physical→logical remaps (nil clears them). When two
// physical keys map to one logical key, the display picks the first in
// sorted order, deterministically.
func SetKeyMap(m map[string]string) {
	toLogical, toPhysical = map[string]string{}, map[string]string{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, raw := range keys {
		phys, logical := pressed(raw), pressed(m[raw])
		if phys == "" || logical == "" || phys == logical {
			continue
		}
		toLogical[phys] = logical
		if _, taken := toPhysical[logical]; !taken {
			toPhysical[logical] = phys
		}
	}
}

// LogicalKey translates a pressed key to the key DHI acts on.
func LogicalKey(k string) string {
	if l, ok := toLogical[k]; ok {
		return l
	}
	return k
}

// DisplayKey names the key to press for logical key k: its remap when
// there is one. A key whose own press was remapped elsewhere still shows
// as itself (it is what the user sees in docs); the remap table is the
// user's own, shown in Settings.
func DisplayKey(k string) string {
	caret := strings.HasPrefix(k, "^") && len(k) > 1
	lookup := pressed(k)
	if caret {
		lookup = "ctrl+" + strings.ToLower(k[1:])
	}
	p, ok := toPhysical[lookup]
	if !ok {
		return k
	}
	if p == " " {
		return "space"
	}
	if caret && strings.HasPrefix(p, "ctrl+") {
		return "^" + strings.TrimPrefix(p, "ctrl+")
	}
	return p
}

// DisplayKeys rewrites a key part of a hint ("s/S", "h/l", "[ ]",
// "ctrl+w v") through DisplayKey, keeping its separators.
func DisplayKeys(part string) string {
	if len(toPhysical) == 0 {
		return part
	}
	var b strings.Builder
	tok := strings.Builder{}
	flush := func() {
		if tok.Len() > 0 {
			b.WriteString(DisplayKey(tok.String()))
			tok.Reset()
		}
	}
	for _, r := range part {
		if r == ' ' || r == '/' || r == '·' {
			flush()
			b.WriteRune(r)
			continue
		}
		tok.WriteRune(r)
	}
	flush()
	return b.String()
}

// DisplayHint rewrites the key part of a chrome hint ("n new" → "x new"
// when n is reached through x).
func DisplayHint(h string) string {
	if len(toPhysical) == 0 {
		return h
	}
	rows := HintRows(h)
	if len(rows) == 0 {
		return h
	}
	k, desc := rows[0][0], rows[0][1]
	if desc == "" {
		return DisplayKeys(k)
	}
	return DisplayKeys(k) + " " + desc
}

// pressed spells a key the way the shell reports a press: the space bar
// arrives as " ", while config files and hints say "space".
func pressed(k string) string {
	if k == "space" {
		return " "
	}
	return k
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
