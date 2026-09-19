package reviewer

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/gitdiff"
)

// BenchmarkDiffRowsCache pins the F-026 P6 flatten cache: a warm
// render (per keystroke) must not re-walk the diff.
func BenchmarkDiffRowsCache(b *testing.B) {
	m := &Model{openID: "r1", layout: layoutUnified}
	for i := 0; i < 20; i++ {
		m.files = append(m.files, gitdiff.FileDiff{
			NewPath: "pkg/file" + itoa(i) + ".go",
			Hunks: []gitdiff.Hunk{
				{Header: "func", Lines: makeLines(40)},
			},
		})
	}
	b.ReportAllocs()
	for b.Loop() {
		rows := m.diffRows()
		if len(rows) == 0 {
			b.Fatal("no rows")
		}
	}
}

// BenchmarkDiffRowsCold measures the cold flatten for comparison.
func BenchmarkDiffRowsCold(b *testing.B) {
	m := &Model{openID: "r1", layout: layoutUnified}
	for i := 0; i < 20; i++ {
		m.files = append(m.files, gitdiff.FileDiff{
			NewPath: "pkg/file" + itoa(i) + ".go",
			Hunks: []gitdiff.Hunk{
				{Header: "func", Lines: makeLines(40)},
			},
		})
	}
	b.ReportAllocs()
	for b.Loop() {
		m.rowsFP = ""
		rows := m.diffRows()
		if len(rows) == 0 {
			b.Fatal("no rows")
		}
	}
}

func makeLines(n int) []gitdiff.Line {
	out := make([]gitdiff.Line, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, gitdiff.Line{Kind: gitdiff.Ctx, OldNo: i + 1, NewNo: i + 1,
			Text: "x = call()"})
	}
	return out
}
