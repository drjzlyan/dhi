// Package version holds the build identity of DHI. Release builds stamp
// these via -ldflags "-X github.com/drjzlyan/dhi/internal/version.Version=…"
// (scripts/build-release.sh); a plain `go build` reports the defaults.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Version is the semantic version of this build.
var Version = "0.1.0"

// Commit and Date identify a release build; "unknown" for local builds.
var (
	Commit = "unknown"
	Date   = "unknown"
)

func init() {
	// A `go install …@vX` or source build carries no ldflags: take the
	// module version and VCS stamp Go records instead of claiming 0.1.0.
	if Commit != "unknown" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		Version = strings.TrimPrefix(v, "v")
	} else {
		Version += "-dev"
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) > 12 {
				Commit = s.Value[:12]
			} else {
				Commit = s.Value
			}
		case "vcs.time":
			Date = s.Value
		}
	}
}

// String is the one-line identity printed by `dhi version`.
func String() string {
	return fmt.Sprintf("dhi %s (commit %s, built %s) %s/%s",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}
