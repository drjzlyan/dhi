package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// bigBuffer opens a synthetic 5k-line buffer for render benchmarks.
func bigBuffer(t testing.TB, m *Model) {
	path := filepath.Join(t.TempDir(), "big.go")
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "func example%03d() { x := %d; _ = x } // line\n", i%100, i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := m.OpenPaths(path); n != 1 {
		t.Fatal("buffer did not open")
	}
}

func BenchmarkBufferView5000(b *testing.B) {
	t := &testing.T{}
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	m := New("bench", ws)
	m.Resize(100, 30)
	bigBuffer(t, m)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.bufferView()
	}
}

func BenchmarkUpdateKey5000(b *testing.B) {
	t := &testing.T{}
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	m := New("bench", ws)
	m.Resize(100, 30)
	bigBuffer(t, m)
	m.HandleKey("i") // insert mode
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.HandleKey("j")
	}
}

func BenchmarkTextbufLargeEdit(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&sb, "line %d with some content\n", i)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ed := textbuf.NewEditor(sb.String())
		ed.Key("i")
		for k := 0; k < 50; k++ {
			ed.Key("x")
		}
		ed.Key("escape")
		ed.Key("u")
	}
}

func TestTabStripElidesOverflow(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	mk := func(vp string) *bufTab {
		return &bufTab{ed: textbuf.NewEditor("x\n"), vp: vp, path: vp}
	}
	// Few tabs, wide room: everything visible, order preserved.
	bufs := []*bufTab{mk("a.go"), mk("b.go"), mk("c.go")}
	got := tabStrip(bufs, 1, 200)
	for _, want := range []string{"a.go", "b.go", "c.go"} {
		if !strings.Contains(ansi.Strip(got), want) {
			t.Errorf("missing %q in %q", want, ansi.Strip(got))
		}
	}
	// Many long tabs, tiny room: active stays visible, overflow elided,
	// and the row never exceeds the budget.
	bufs = make([]*bufTab, 0, 30)
	for i := 0; i < 30; i++ {
		bufs = append(bufs, mk(fmt.Sprintf("really-long-name-%02d.go", i)))
	}
	avail := 60
	got = ansi.Strip(tabStrip(bufs, 15, avail))
	if !strings.Contains(got, "really-long-name-15.go") {
		t.Errorf("active tab dropped: %q", got)
	}
	if !strings.Contains(got, "+") {
		t.Errorf("no elision marker: %q", got)
	}
	if w := lipgloss.Width(tabStrip(bufs, 15, avail)); w > avail {
		t.Errorf("strip width %d exceeds avail %d", w, avail)
	}
	// Active at the far end: left marker, active anchored.
	got = ansi.Strip(tabStrip(bufs, 29, avail))
	if !strings.Contains(got, "really-long-name-29.go") || !strings.Contains(got, "…+") {
		t.Errorf("end-anchored strip wrong: %q", got)
	}
}
