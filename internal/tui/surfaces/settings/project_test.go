package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

func projectSurface(t *testing.T) (*Model, string) {
	t.Helper()
	m, ws, _, _ := agentSurface(t)
	for m.sec != secProject {
		feed(m, "[")
	}
	return m, ws.Root
}

func TestProjectEmptyStateAndDraftFromRepo(t *testing.T) {
	m, root := projectSurface(t)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "No brief yet") {
		t.Fatalf("empty state missing:\n%s", v)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", "go.mod"), []byte("module x/shop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	feed(m, "d")
	if m.dkind != dlgProject || m.dform.Values()[4] != "Go" {
		t.Fatalf("draft not prefilled: %v", m.dform.Values())
	}
	feed(m, "enter")
	b, ok, err := brief.Load(root)
	if err != nil || !ok || b.Stack != "Go" || b.Process != brief.DefaultProcess {
		t.Fatalf("saved %+v ok=%v err=%v", b, ok, err)
	}
	golden.Snapshot(t, "settings_project", m.View())
}

func TestProjectEditKeepsNewlines(t *testing.T) {
	m, root := projectSurface(t)
	_ = brief.Save(root, brief.Brief{Name: "shop", Process: "a\nb"})
	feed(m, "e")
	if got := m.dform.Values()[5]; got != `a\nb` {
		t.Fatalf("process field %q", got)
	}
	golden.Snapshot(t, "settings_project_edit", m.View())
	feed(m, "enter")
	b, _, _ := brief.Load(root)
	if b.Process != "a\nb" || b.Name != "shop" {
		t.Fatalf("round trip %+v", b)
	}
}

func TestProjectMalformedBriefIsNamed(t *testing.T) {
	m, root := projectSurface(t)
	_ = os.WriteFile(brief.Path(root), []byte("schema = 1\n[project]\nbogus = 1\n"), 0o644)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "project.bogus") {
		t.Fatalf("malformed brief not named:\n%s", v)
	}
}

func TestProjectInterviewAsksTheLead(t *testing.T) {
	m, root := projectSurface(t)
	feed(m, "i")
	if !strings.Contains(m.flash, "chat is unavailable") {
		t.Fatalf("flash %q", m.flash)
	}
	b, err := bus.Open(m.d.WS)
	if err != nil {
		t.Fatal(err)
	}
	m.d.Bus = b
	feed(m, "i")
	if !strings.Contains(m.flash, "no team has an employee as lead") {
		t.Fatalf("flash %q", m.flash)
	}
	writeRosterAgent(t, root, "atlas")
	if err := m.d.Org.CreateTeam("core", "atlas", []string{"atlas"}); err != nil {
		t.Fatal(err)
	}
	feed(m, "i")
	if !strings.Contains(m.flash, "once employees are running") {
		t.Fatalf("flash %q", m.flash)
	}
	rt := &fakeTurns{}
	m.d.Runtime = rt
	feed(m, "i")
	if !strings.Contains(m.flash, "asked atlas to interview you") || len(rt.got) != 1 ||
		!strings.HasPrefix(rt.got[0].Text, "@atlas Please interview me") || rt.got[0].ID == 0 {
		t.Fatalf("flash %q handled %+v", m.flash, rt.got)
	}
	hist := b.History("#core", 0)
	if len(hist) != 2 || !strings.HasPrefix(hist[1].Text, "@atlas Please interview me") || hist[1].Author != bus.Human {
		t.Fatalf("history %+v", hist)
	}
}

// fakeTurns records what Settings hands to the runtime.
type fakeTurns struct{ got []bus.Message }

func (f *fakeTurns) Handle(_ context.Context, msg bus.Message) { f.got = append(f.got, msg) }
