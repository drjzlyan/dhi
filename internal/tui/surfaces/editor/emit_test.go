package editor

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/dap"
	"github.com/drjzlyan/dhi/internal/testutil/dapfake"
	"github.com/drjzlyan/dhi/internal/tutorial"
)

// recorder returns an emitter that appends to the returned slice.
func recorder(m *Model) *[]string {
	var got []string
	m.SetEmitter(func(ev string) { got = append(got, ev) })
	return &got
}

func TestEditorReportsTheActionsALessonWaitsFor(t *testing.T) {
	m := newEditor(t)
	got := recorder(m)

	feed(m, "enter", "down", "down", "enter") // open a file
	if !slices.Contains(*got, tutorial.EvEditorOpen) {
		t.Fatalf("opening a file reported %v", *got)
	}
	if slices.Contains(*got, tutorial.EvEditorInsert) {
		t.Fatalf("insert reported before typing: %v", *got)
	}
	feed(m, "i")
	if !slices.Contains(*got, tutorial.EvEditorInsert) {
		t.Fatalf("entering insert mode reported %v", *got)
	}
	feed(m, "esc")
	feed(m, ":")
	typeKeys(m, "w")
	feed(m, "enter")
	if !slices.Contains(*got, tutorial.EvEditorSave) {
		t.Fatalf("saving reported %v", *got)
	}
	feed(m, ":")
	typeKeys(m, "break")
	feed(m, "enter")
	if !slices.Contains(*got, tutorial.EvEditorBreak) {
		t.Fatalf("setting a breakpoint reported %v", *got)
	}
}

func TestRefusedOrNoOpCommandsReportNothing(t *testing.T) {
	m := newEditor(t)
	got := recorder(m)
	feed(m, "enter", "down", "down", "enter")
	*got = nil
	feed(m, ":")
	typeKeys(m, "pair nobody") // no crew wired: refused
	feed(m, "enter")
	feed(m, ":")
	typeKeys(m, "fmt") // no server: refused
	feed(m, "enter")
	for _, ev := range []string{tutorial.EvEditorPair, tutorial.EvEditorFormat, tutorial.EvEditorTest, tutorial.EvEditorDebug} {
		if slices.Contains(*got, ev) {
			t.Errorf("a refused command reported %q", ev)
		}
	}
}

func TestEditorWithoutAnEmitterStillWorks(t *testing.T) {
	m := newEditor(t) // never wired: act() must be a no-op
	feed(m, "enter", "down", "down", "enter", "i", "esc")
}

func TestTestPairFormatAndDebugRunsReportTheirAction(t *testing.T) {
	t.Run("test", func(t *testing.T) {
		m := testEditor(t, stubGo(t, `{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0}`))
		got := recorder(m)
		runEx(m, "test all")
		if !slices.Contains(*got, tutorial.EvEditorTest) {
			t.Fatalf("starting tests reported %v", *got)
		}
		waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "tests passed") }, "run finished")
	})
	t.Run("pair", func(t *testing.T) {
		h, _ := pairChatEditor(t)
		got := recorder(h.m)
		typeKeys(h.m, ":pair scout")
		h.m.HandleKey("enter")
		if !slices.Contains(*got, tutorial.EvEditorPair) {
			t.Fatalf("pairing reported %v", *got)
		}
	})
	t.Run("format", func(t *testing.T) {
		m, _ := openMainGoWithLSP(t)
		got := recorder(m)
		runEx(m, "fmt")
		if !slices.Contains(*got, tutorial.EvEditorFormat) {
			t.Fatalf("formatting reported %v (msg %q)", *got, m.active().Message())
		}
	})
	t.Run("debug", func(t *testing.T) {
		m := testEditor(t, nil)
		path := m.active().Path()
		m.dapStart = func(context.Context, string, []string) (*dap.Client, error) {
			conn, a := dapfake.New(t)
			a.StopAt(path, 1)
			return dap.New(conn), nil
		}
		got := recorder(m)
		runEx(m, "break")
		runEx(m, "debug")
		waitFor(t, m, func() bool { return m.mode == modeDebug }, "stop at breakpoint")
		if !slices.Contains(*got, tutorial.EvEditorDebug) {
			t.Fatalf("debugging reported %v", *got)
		}
	})
}
