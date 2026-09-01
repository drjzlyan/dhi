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
	lines []string
	isMD  bool
}

// previewCache maps "path\x00hash\x00width" → rendered lines.
var previewCache = map[string]previewEntry{}

// loadPreview renders the artifact under the ARTIFACTS cursor. Markdown
// gets the GitHub-style glamour treatment; anything else renders raw.
func (m *Model) loadPreview(width int) (previewEntry, bool) {
	rel, ok := m.artifactRelAt(m.cursors[secArtifacts])
	if !ok || m.store == nil {
		return previewEntry{}, false
	}
	abs := m.store.ArtifactPath(m.openID, rel)
	data, err := os.ReadFile(abs)
	if err != nil {
		return previewEntry{lines: []string{theme.DangerText().Render("unreadable: " + err.Error())}}, true
	}
	sum := sha256.Sum256(data)
	key := abs + "\x00" + string(sum[:]) + "\x00" + itoa(width)
	if e, ok := previewCache[key]; ok {
		return e, true
	}
	text := string(data)
	var lines []string
	isMD := preview.IsMarkdown(rel)
	if isMD {
		rendered, rerr := preview.Render(text, width)
		if rerr != nil {
			lines = []string{theme.DangerText().Render("render failed: " + rerr.Error())}
		} else {
			lines = strings.Split(rendered, "\n")
		}
	} else {
		for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			lines = append(lines, theme.TextDim().Render(l))
		}
	}
	e := previewEntry{lines: lines, isMD: isMD}
	previewCache[key] = e
	return e, true
}

func (m *Model) previewHeight() int {
	return maxInt(m.height-6, 4)
}

func (m *Model) previewLines() []string {
	w := maxInt(m.width-railWidth-8, 40)
	e, _ := m.loadPreview(w)
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

func (m *Model) previewBody(w, h int) string {
	rel, ok := m.artifactRelAt(m.cursors[secArtifacts])
	if !ok {
		out := []string{theme.Hint().Render("artifact preview")}
		out = append(out, theme.TextDim().Render("(no artifact selected — pick one under ARTIFACTS)"))
		return strings.Join(out, "\n")
	}
	e, ok := m.loadPreview(w)
	if !ok {
		return ""
	}
	head := theme.Hint().Render(rel)
	if e.isMD {
		head += theme.TextDim().Render("  (markdown)")
	} else {
		head += theme.TextDim().Render("  (raw)")
	}
	out := []string{head}
	total := len(e.lines)
	top := m.previewTop
	if top > total {
		top = 0
	}
	for i := top; i < total && len(out) < h+1; i++ {
		out = append(out, e.lines[i])
	}
	if top+m.previewHeight() < total {
		out = append(out, theme.Hint().Render(
			"… "+itoa(total-top-m.previewHeight())+" more (j/k scroll)"))
	}
	return strings.Join(out, "\n")
}
