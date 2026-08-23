package bootstrap

import (
	"errors"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/toolchain"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// plain renders a view as it reads without ANSI styling.
func plain(m *Model) string { return ansi.Strip(m.View()) }

func rowText(glyph, label string) string {
	return strings.TrimSpace(glyph) + "  " + label
}

func feed(m *Model, events ...toolchain.Event) {
	for _, ev := range events {
		m.Update(eventMsg(ev))
	}
}

func newTestModel() *Model {
	return &Model{
		version: "0.1.0",
		rows:    map[string]*row{},
		events:  make(chan eventMsg, 64),
	}
}

func TestMetaAndKeys(t *testing.T) {
	m := newTestModel()
	meta := m.Meta()
	if meta.ID != "bootstrap" || meta.Title != "Bootstrap" {
		t.Errorf("meta = %+v", meta)
	}
	if m.HandleKey("q") {
		t.Error("HandleKey consumed a key it should not")
	}
}

func TestViewProgressesThroughEvents(t *testing.T) {
	m := newTestModel()
	v := m.View()
	if !strings.Contains(v, "preparing hermetic toolchain") {
		t.Errorf("initial view missing headline:\n%s", v)
	}

	feed(m,
		toolchain.Event{Kind: toolchain.EventManifestFetched, Detail: "http://127.0.0.1/manifest.json"},
		toolchain.Event{Kind: toolchain.EventResolved, Detail: "2 action(s)"},
	)
	v = plain(m)
	if !strings.Contains(v, rowText(theme.GlyphCheck, "registry manifest")) ||
		!strings.Contains(v, "2 action(s)") {
		t.Errorf("registry/plan rows not rendered:\n%s", v)
	}

	feed(m, toolchain.Event{Kind: toolchain.EventDownloadStart, Tool: "rg"})
	if !strings.Contains(plain(m), spinnerFrames[0]) {
		t.Errorf("spinner glyph frame 0 missing:\n%s", m.View())
	}
	if strings.Contains(plain(m), theme.GlyphCheck+"  rg") {
		t.Error("rg reported done while still downloading")
	}

	for i := 0; i < 3; i++ {
		if cmd := m.Update(tickMsg{}); cmd == nil {
			t.Fatal("running phase must re-arm the tick")
		} else {
			_ = cmd
		}
	}
	if !strings.Contains(m.View(), spinnerFrames[3]) {
		t.Errorf("spinner did not advance to frame 3:\n%s", m.View())
	}

	feed(m,
		toolchain.Event{Kind: toolchain.EventDownloadDone, Tool: "rg"},
		toolchain.Event{Kind: toolchain.EventVerified, Tool: "rg"},
		toolchain.Event{Kind: toolchain.EventExtracted, Tool: "rg"},
		toolchain.Event{Kind: toolchain.EventActivated, Tool: "rg"},
		toolchain.Event{Kind: toolchain.EventToolDone, Tool: "rg"},
	)
	v = plain(m)
	if !strings.Contains(v, rowText(theme.GlyphCheck, "rg")) || !strings.Contains(v, "installed") {
		t.Errorf("rg completion row missing:\n%s", v)
	}

	cmd := m.Update(installDoneMsg{})
	if cmd != nil {
		t.Error("done must stop the animation clock")
	}
	if !strings.Contains(plain(m), "toolchain ready") {
		t.Errorf("ready headline missing:\n%s", m.View())
	}
}

func TestUpToDatePath(t *testing.T) {
	m := newTestModel()
	feed(m,
		toolchain.Event{Kind: toolchain.EventManifestFetched},
		toolchain.Event{Kind: toolchain.EventResolved, Detail: "0 action(s)"},
		toolchain.Event{Kind: toolchain.EventDone, Detail: "up to date"},
	)
	m.Update(installDoneMsg{})
	v := plain(m)
	if !strings.Contains(v, "up to date") || !strings.Contains(v, "toolchain ready") {
		t.Errorf("up-to-date view:\n%s", v)
	}
}

func TestFailureMarksActiveRows(t *testing.T) {
	m := newTestModel()
	feed(m,
		toolchain.Event{Kind: toolchain.EventManifestFetched},
		toolchain.Event{Kind: toolchain.EventDownloadStart, Tool: "rg"},
	)
	m.Update(installDoneMsg{err: errors.New("sha256 mismatch for artifact.tar.gz")})

	v := m.View()
	if !strings.Contains(v, "bootstrap failed") {
		t.Errorf("failure headline missing:\n%s", v)
	}
	if !strings.Contains(v, theme.GlyphCross) || !strings.Contains(v, "rg download") {
		t.Errorf("failed row glyph missing:\n%s", v)
	}
	if !strings.Contains(v, "sha256 mismatch") {
		t.Errorf("error detail missing:\n%s", v)
	}
	if cmd := m.Update(tickMsg{}); cmd != nil {
		t.Error("tick re-armed after failure; clock must stop")
	}
}

func TestFinishedGatesRelease(t *testing.T) {
	m := newTestModel()
	if m.Finished() {
		t.Fatal("fresh model reports finished before any event")
	}
	feed(m, toolchain.Event{Kind: toolchain.EventManifestFetched})
	if m.Finished() {
		t.Fatal("running install reports finished")
	}
	m.Update(installDoneMsg{})
	if !m.Finished() {
		t.Error("completed install must release the gate")
	}

	f := newTestModel()
	f.Update(installDoneMsg{err: errors.New("no network")})
	if !f.Finished() {
		t.Error("failed install must also release the gate (degrade visibly)")
	}
}

func TestLongErrorTruncated(t *testing.T) {
	m := newTestModel()
	long := strings.Repeat("x", 200)
	m.Update(installDoneMsg{err: errors.New(long)})
	v := m.View()
	if strings.Contains(v, long) {
		t.Error("unbounded error leaked into view")
	}
	if !strings.Contains(v, "…") {
		t.Error("truncation marker missing")
	}
}
