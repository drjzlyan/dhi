package doctor

import (
	"regexp"
	"sort"
	"strings"
)

// tokenNameRe matches env var names that usually hold a credential.
var tokenNameRe = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY|ACCESS_?KEY|PRIVATE_?KEY|CREDENTIAL)`)

// Secrets reports exported environment variables that look like
// credentials (F-089, ADR-0039): agents inherit DHI's environment, so
// these reach every agent CLI as is, outside the redacted, by-name path.
// Names only, never values. A warning, not a failure: you chose to keep
// the environment inherited.
func Secrets(environ []string) []Check {
	var names []string
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		if value != "" && tokenNameRe.MatchString(name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return []Check{{Name: "secrets/host-env", Status: OK, Detail: "no exported variable looks like a credential"}}
	}
	sort.Strings(names)
	return []Check{{Name: "secrets/host-env", Status: Warn, Detail: strings.Join(names, ", ") +
		" are exported and every agent inherits them; store them in Settings → SECRETS and unset them to keep them by-name and redacted"}}
}
