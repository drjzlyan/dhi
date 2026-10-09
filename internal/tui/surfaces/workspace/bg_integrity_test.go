package workspace

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

// F-064: every section, with content and with a dialog open, paints a
// consistent background inside its boxes and keeps rows inside borders.
func TestBackgroundIntegrityAcrossSections(t *testing.T) {
	m, ws, _ := newSurfaceWithBus(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	for _, s := range []string{"login", "audit-log", "csv"} {
		if err := store.Create(s, "Title "+s, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, sec := range []sectionID{secInbox, secBoard, secChannels, secRepos} {
		m.sec = sec
		golden.AssertBgIntegrity(t, "section "+sec.label(), m.View())
	}
	m.sec = secBoard
	m.HandleKey("n")
	golden.AssertBgIntegrity(t, "board + new-task dialog", m.View())
	m.HandleKey("esc")
	m.Resize(72, 30)
	golden.AssertBgIntegrity(t, "board narrow", m.View())
}
