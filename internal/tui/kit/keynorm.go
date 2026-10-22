package kit

import "strings"

// Key normalization (F-101). Terminals disagree about modifier keys, and
// macOS ones most of all: without "Option as Meta", Terminal.app and
// iTerm2 send Option+1 as the character "¡", not alt+1; with it,
// Option+← arrives as ESC b (alt+b) rather than alt+left. The shell
// normalizes every key once, before any surface or binding sees it, so a
// binding names the key the user meant.

// optionDigits are the characters the US macOS layout types for
// Option+1..0. Other layouts differ; help explains Option as Meta for them.
var optionDigits = map[string]string{
	"¡": "alt+1", "™": "alt+2", "£": "alt+3", "¢": "alt+4", "∞": "alt+5",
	"§": "alt+6", "¶": "alt+7", "•": "alt+8", "ª": "alt+9", "º": "alt+0",
}

// wordKeys are the emacs-style meta keys macOS terminals send for
// Option+arrow when Option is Meta.
var wordKeys = map[string]string{"alt+b": "alt+left", "alt+f": "alt+right"}

// MacKeys renders alt as ⌥ in hints (set by cmd on darwin; tests and
// goldens keep the portable spelling).
var MacKeys bool

// NormalizeKey maps a pressed key to the key DHI acts on. raw is true
// while a terminal has focus: its program gets exactly what was pressed.
// takingText is true while a text field or insert-mode buffer has focus:
// an Option character is then text, not a shortcut. option reports that
// an Option character stood in for alt+digit, so the shell can explain
// it once.
func NormalizeKey(key string, takingText, raw bool) (norm string, option bool) {
	if raw {
		return key, false
	}
	if w, ok := wordKeys[key]; ok {
		return w, false
	}
	if !takingText {
		if a, ok := optionDigits[key]; ok {
			return a, true
		}
	}
	return key, false
}

// IsOptionDigit reports whether key is a character macOS types for
// Option+digit on the US layout.
func IsOptionDigit(key string) bool {
	_, ok := optionDigits[key]
	return ok
}

// AltLabel spells alt chords in a hint the platform's way: "⌥1" on macOS,
// "alt+1" elsewhere.
func AltLabel(s string) string {
	if !MacKeys {
		return s
	}
	return strings.ReplaceAll(s, "alt+", "⌥")
}
