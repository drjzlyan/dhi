package reviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitdiff"
)

// twoHunkPatch changes line 4 and adds a line after old 20 (so everything
// below sits one line lower on the new side).
const twoHunkPatch = `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -3,3 +3,3 @@
 line3
-line4
+LINE4
 line5
@@ -19,3 +19,4 @@
 line19
 line20
+extra
 line21
`

// gapModel opens a review whose worktree holds a 30-line main.go matching
// twoHunkPatch's new side.
func gapModel(t *testing.T, withSource bool) *Model {
	t.Helper()
	m, ws, st, _ := newSurface(t)
	r := startBranchReview(t, m, st)
	if withSource {
		var sb strings.Builder
		for n := 1; n <= 31; n++ {
			switch {
			case n == 4:
				sb.WriteString("LINE4\n")
			case n == 22:
				sb.WriteString("extra\n")
			case n > 22:
				fmt.Fprintf(&sb, "line%d\n", n-1)
			default:
				fmt.Fprintf(&sb, "line%d\n", n)
			}
		}
		dir := filepath.Join(ws.Root, filepath.FromSlash(r.WorkRel))
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.files = gitdiff.Parse(twoHunkPatch)
	m.layout = layoutUnified
	m.resetGapState()
	return m
}

func gapRowsOf(m *Model) []viewRow {
	var out []viewRow
	for _, r := range m.diffRows() {
		if r.kind == vrGap {
			out = append(out, r)
		}
	}
	return out
}

func TestCollapsedGapsSitBeforeBetweenAndAfterHunks(t *testing.T) {
	m := gapModel(t, true)
	gs := gapRowsOf(m)
	if len(gs) != 3 {
		t.Fatalf("gap rows = %d, want 3", len(gs))
	}
	for i, want := range []string{"2 unchanged lines", "13 unchanged lines", "9 unchanged lines"} {
		if !strings.Contains(gs[i].text, want) {
			t.Errorf("gap %d = %q, want %q", i, gs[i].text, want)
		}
	}
}

func TestExpandingAGapShowsContextWithBothLineNumbers(t *testing.T) {
	m := gapModel(t, true)
	before := len(m.diffRows())
	m.expandGap(gapRowsOf(m)[2].gap) // the trailing region, below the added line
	rows := m.diffRows()
	if len(rows) != before-1+9 {
		t.Fatalf("rows %d → %d, want +8", before, len(rows))
	}
	var first *gitdiff.Line
	for _, r := range rows {
		if r.kind == vrLineUnified && r.left.Kind == gitdiff.Ctx && r.left.NewNo == 23 {
			first = r.left
		}
	}
	if first == nil {
		t.Fatal("expanded line 23 missing")
	}
	// An added line sits above, so old = new - 1.
	if first.OldNo != 22 || first.Text != "line22" {
		t.Fatalf("line = %+v", *first)
	}
	if len(gapRowsOf(m)) != 2 {
		t.Fatal("expanded region still shows a collapsed row")
	}
}

func TestEKeyExpandsTheGapUnderTheCursor(t *testing.T) {
	m := gapModel(t, true)
	m.sec = secDiff
	rows := m.diffRows()
	for i, r := range rows {
		if r.kind == vrGap {
			m.cursor = i
			break
		}
	}
	m.HandleKey("e")
	if len(gapRowsOf(m)) != 2 {
		t.Fatalf("e did not expand: %d gaps left", len(gapRowsOf(m)))
	}
}

func TestNoSourceMeansNoGaps(t *testing.T) {
	m := gapModel(t, false)
	if gs := gapRowsOf(m); len(gs) != 0 {
		t.Fatalf("gaps without a worktree copy: %d", len(gs))
	}
}

func TestGapsNeverReadOutsideTheWorktree(t *testing.T) {
	m := gapModel(t, true)
	m.files = gitdiff.Parse(strings.ReplaceAll(twoHunkPatch, "main.go", "../../etc/hosts"))
	m.resetGapState()
	if m.sourceLines(0) != nil {
		t.Fatal("read a path outside the review worktree")
	}
}

func TestExpandedGapsRenderInBothLayouts(t *testing.T) {
	for _, lay := range []layoutMode{layoutUnified, layoutSideBySide} {
		m := gapModel(t, true)
		m.layout = lay
		for _, g := range gapRowsOf(m) {
			m.expandGap(g.gap)
		}
		m.sec = secDiff
		out := ansiStrip(m.View())
		if !strings.Contains(out, "line10") {
			t.Errorf("layout %v: expanded context not rendered:\n%s", lay, out)
		}
	}
}
