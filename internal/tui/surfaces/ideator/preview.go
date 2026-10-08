package ideator

import (
	"crypto/sha256"
	"os"
	"strings"

	"github.com/drjzlyan/dhi/internal/preview"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// previewEntry memoizes one rendered artifact (same content + width →
// identical render, so goldens stay stable across repaints).
type previewEntry struct {
	lines     []string
	isMD      bool
	isMermaid bool
}

// previewCache maps "path\x00hash\x00width" → rendered lines.
var previewCache = map[string]previewEntry{}

// loadPreview renders the artifact under the CANVAS cursor. Markdown
// gets the GitHub-style glamour treatment; mermaid diagrams get the
// deterministic ASCII outline (F-033 Part B); anything else renders raw.
func (m *Model) loadPreview(width int) previewEntry {
	rel, ok := m.artifactRelAt(m.cursors[secCanvas])
	if !ok || m.store == nil {
		return previewEntry{}
	}
	abs := m.store.ArtifactPath(m.openID, rel)
	data, err := os.ReadFile(abs)
	if err != nil {
		return previewEntry{lines: []string{theme.DangerText().Render("unreadable: " + err.Error())}}
	}
	sum := sha256.Sum256(data)
	key := abs + "\x00" + string(sum[:]) + "\x00" + itoa(width)
	if e, ok := previewCache[key]; ok {
		return e
	}
	text := string(data)
	var lines []string
	isMD := preview.IsMarkdown(rel)
	isMermaid := preview.IsMermaid(rel)
	switch {
	case isMermaid:
		rendered, rerr := preview.RenderMermaid(text, width)
		if rerr != nil {
			lines = []string{theme.DangerText().Render("render failed: " + rerr.Error())}
		} else {
			lines = strings.Split(strings.TrimRight(rendered, "\n"), "\n")
		}
	case isMD:
		rendered, rerr := preview.Render(text, width)
		if rerr != nil {
			lines = []string{theme.DangerText().Render("render failed: " + rerr.Error())}
		} else {
			lines = strings.Split(rendered, "\n")
		}
	default:
		for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			lines = append(lines, theme.TextDim().Render(l))
		}
	}
	e := previewEntry{lines: lines, isMD: isMD, isMermaid: isMermaid}
	previewCache[key] = e
	return e
}

func (m *Model) previewHeight() int {
	return maxInt(m.height-6, 4)
}

func (m *Model) previewLines() []string {
	w := maxInt(m.width-railWidth-8, 40)
	e := m.loadPreview(w)
	return e.lines
}

func (m *Model) clampPreview() {
	n := len(m.previewLines())
	maxTop := maxInt(0, n-m.previewHeight())
	if m.previewTop > maxTop {
		m.previewTop = maxTop
	}
	if m.previewTop < 0 {
		m.previewTop = 0
	}
}
