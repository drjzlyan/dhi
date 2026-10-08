package clirun

import (
	"fmt"
	"strconv"
	"strings"
)

// Verdict is how a found CLI version relates to the one an adapter was
// verified against (ADR-0027 §6).
type Verdict uint8

const (
	// VerdictExact: the verified version.
	VerdictExact Verdict = iota
	// VerdictDrift: same major, different minor/patch — newer or older than
	// verified. Usable; worth a warning.
	VerdictDrift
	// VerdictMajor: a different major — behaviour may have changed; refuse
	// until the adapter is re-verified.
	VerdictMajor
	// VerdictUnknown: the version could not be compared (unparseable and not
	// equal).
	VerdictUnknown
)

// parseVersion extracts up to three leading numeric components.
func parseVersion(v string) ([3]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out [3]int
	parts := strings.SplitN(v, ".", 4)
	if len(parts) == 0 {
		return out, false
	}
	n := 0
	for i := 0; i < 3 && i < len(parts); i++ {
		digits := parts[i]
		for j, r := range digits { // "1-beta" → "1"
			if r < '0' || r > '9' {
				digits = digits[:j]
				break
			}
		}
		if digits == "" {
			break
		}
		x, err := strconv.Atoi(digits)
		if err != nil {
			return out, false
		}
		out[i] = x
		n++
	}
	return out, n > 0
}

// Assess compares a found version with the adapter's tested one and says
// what to do about it, in words a doctor row or wizard can show.
func Assess(tested, found string) (Verdict, string) {
	if found == tested {
		return VerdictExact, "verified version"
	}
	tv, tok := parseVersion(tested)
	fv, fok := parseVersion(found)
	if !tok || !fok {
		return VerdictUnknown, fmt.Sprintf("%s is not comparable with the verified %s", found, tested)
	}
	if tv[0] != fv[0] {
		return VerdictMajor, fmt.Sprintf("%s is a different major version than the verified %s", found, tested)
	}
	rel := relation(tv, fv)
	return VerdictDrift, fmt.Sprintf("%s is %s the verified %s (same major, expected to work)", found, rel, tested)
}

// relation words how fv sits against tv within one major: "newer than",
// "older than" or "a variant of".
func relation(tv, fv [3]int) string {
	if fv == tv { // e.g. a suffix-only difference
		return "a variant of"
	}
	for i := 1; i < 3; i++ {
		if fv[i] > tv[i] {
			return "newer than"
		}
		if fv[i] < tv[i] {
			return "older than"
		}
	}
	return "a variant of"
}

// Relation is the short form of a same-major drift ("newer than",
// "older than", "a variant of"), for compact UI; "" when the versions are
// equal or not same-major comparable.
func Relation(tested, found string) string {
	if v, _ := Assess(tested, found); v != VerdictDrift {
		return ""
	}
	tv, _ := parseVersion(tested)
	fv, _ := parseVersion(found)
	return relation(tv, fv)
}
