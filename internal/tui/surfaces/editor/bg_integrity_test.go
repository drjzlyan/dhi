package editor

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

// F-064: tree, buffer, split and terminal drawer keep every cell inside
// their boxes painted and every row inside its border.
func TestBackgroundIntegrity(t *testing.T) {
	m := liveEditor(t)
	golden.AssertBgIntegrity(t, "tree", m.View())
	feed(m, "enter", "j", "enter") // expand the first repo, open a file
	golden.AssertBgIntegrity(t, "tree + buffer", m.View())
	feed(m, "ctrl+t")
	m.Update(teaMsg{kind: termMsgOut, tab: 0, chunk: []byte("\x1b[1;32mok\x1b[0m build\r\n$ ls\r\na  b\r\n$ ")})
	golden.AssertBgIntegrity(t, "drawer", m.View())
	feed(m, "ctrl+t")
	m.Resize(140, 36)
	golden.AssertBgIntegrity(t, "wide", m.View())
	m = newEditor(t)
	m.Resize(160, 30)
	m.OpenPaths(vpathAbs(t, m.ws, "alpha/app.go"), vpathAbs(t, m.ws, "beta/README.md"))
	feed(m, "ctrl+w", "v")
	if !m.splitActive() {
		t.Fatal("split did not open")
	}
	golden.AssertBgIntegrity(t, "split", m.View())
}
