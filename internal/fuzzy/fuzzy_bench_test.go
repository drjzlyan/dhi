package fuzzy

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// benchIndex synthesizes a 20k-entry path index shaped like a real
// multi-repo workspace (directories + files, mixed separators).
func benchIndex() []string {
	const n = 20000
	rng := rand.New(rand.NewSource(42))
	parts := []string{"alpha", "beta", "gamma", "delta", "internal",
		"cmd", "pkg", "tui", "editor", "buffer", "view", "main", "test"}
	exts := []string{".go", ".md", ".toml", ".ts", ".json"}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var sb strings.Builder
		sb.WriteString(parts[rng.Intn(len(parts))])
		for d := 0; d < 3; d++ {
			sb.WriteByte('/')
			sb.WriteString(parts[rng.Intn(len(parts))])
		}
		sb.WriteString(parts[rng.Intn(len(parts))])
		sb.WriteString(exts[rng.Intn(len(exts))])
		out = append(out, sb.String())
	}
	return out
}

func BenchmarkRank20k(b *testing.B) {
	items := benchIndex()
	for _, pat := range []string{"main", "edtr", "internal/edit/buffer"} {
		b.Run(pat, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				Rank(pat, items)
			}
		})
	}
}

func BenchmarkIndexRank20k(b *testing.B) {
	ix := NewIndex(benchIndex())
	for _, pat := range []string{"main", "edtr", "internal/edit/buffer"} {
		b.Run(pat, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ix.Rank(pat)
			}
		})
	}
}

func BenchmarkMatch(b *testing.B) {
	s := "internal/tui/surfaces/editor/bufferview.go"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Match("buffrview", s)
	}
}

func ExampleRank() {
	res := Rank("mv", []string{"main.go", "model/view.go", "readme.md"})
	for _, r := range res {
		fmt.Println(r.Index, r.Score)
	}
	// Output:
	// 1 24
}
