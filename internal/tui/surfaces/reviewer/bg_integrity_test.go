package reviewer

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// F-064: the review screen and the single-column diff keep washes and
// fills inside their panels.
func TestBackgroundIntegrity(t *testing.T) {
	m := screenModel(t, 150)
	golden.AssertBgIntegrity(t, "screen wide", m.View())
	m.HandleKey("\\")
	golden.AssertBgIntegrity(t, "screen side by side", m.View())
	m = screenModel(t, kit.WWide-10)
	golden.AssertBgIntegrity(t, "diff narrow", m.View())
	m.sec = secFiles
	golden.AssertBgIntegrity(t, "files", m.View())
	m.sec = secReviews
	golden.AssertBgIntegrity(t, "reviews", m.View())
}
