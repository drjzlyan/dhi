package editor

import (
	"os"
	"testing"

	"github.com/drjzlyan/dhi/internal/editshim"
)

// The test binary stands in for the DHI binary when git calls the editor
// shim (F-165).
func TestMain(m *testing.M) {
	if editshim.IsShim(os.Args[0]) {
		os.Exit(editshim.RunShim(os.Args[1:], os.Getenv, func(s string) { os.Stderr.WriteString(s + "\n") }))
	}
	os.Exit(m.Run())
}
