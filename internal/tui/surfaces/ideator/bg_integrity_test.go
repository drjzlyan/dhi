package ideator

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

func TestBackgroundIntegrity(t *testing.T) {
	m, _, _, _, _ := newSurface(t)
	for s := sectionID(0); s < secCount; s++ {
		m.sec = s
		golden.AssertBgIntegrity(t, "section", m.View())
	}
	m.sec = secSessions
	m.HandleKey("n")
	golden.AssertBgIntegrity(t, "new session dialog", m.View())
}
