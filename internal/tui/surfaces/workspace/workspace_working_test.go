package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestBoardShowsWorkingIndicator(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard
	store.Create("fix-thing", "Fix the thing", "", "")
	if err := store.BindThread("fix-thing", "#general", 42); err != nil {
		t.Fatal(err)
	}
	m.working = func(ch string, thread int64) bool { return ch == "#general" && thread == 42 }

	out := ansi.Strip(m.boardBody(110, 34))
	if !strings.Contains(out, "agent working") {
		t.Fatalf("working detail line missing:\n%s", out)
	}
	if !strings.Contains(out, "●") {
		t.Fatalf("working card glyph missing:\n%s", out)
	}

	// Idle: no indicator when the working seam reports false.
	m.working = func(string, int64) bool { return false }
	if out := ansi.Strip(m.boardBody(110, 34)); strings.Contains(out, "agent working") {
		t.Fatalf("idle board still shows working:\n%s", out)
	}
	// Nil seam degrades silently.
	m.working = nil
	if out := ansi.Strip(m.boardBody(110, 34)); strings.Contains(out, "agent working") {
		t.Fatalf("nil seam shows working:\n%s", out)
	}
}

func TestBoardCardWorkingGlyph(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	row := ansi.Strip(boardCard(tasks.Task{Slug: "x", Title: "X"}, 40, false, true))
	if !strings.Contains(row, "●") {
		t.Fatalf("working card missing glyph: %q", row)
	}
	idle := ansi.Strip(boardCard(tasks.Task{Slug: "x", Title: "X"}, 40, false, false))
	if strings.Contains(idle, "●") {
		t.Fatalf("idle card has glyph: %q", idle)
	}
}
