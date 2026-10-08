// Package version holds the build identity of DHI. Release builds stamp
// these via -ldflags "-X github.com/drjzlyan/dhi/internal/version.Version=…"
// (scripts/build-release.sh); a plain `go build` reports the defaults.
package version

import (
	"fmt"
	"runtime"
)

// Version is the semantic version of this build.
var Version = "0.1.0"

// Commit and Date identify a release build; "unknown" for local builds.
var (
	Commit = "unknown"
	Date   = "unknown"
)

// String is the one-line identity printed by `dhi version`.
func String() string {
	return fmt.Sprintf("dhi %s (commit %s, built %s) %s/%s",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}
