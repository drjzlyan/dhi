// Package preview renders markdown for the editor's preview pane:
// GitHub-flavored (tables, task lists, strikethrough) via goldmark,
// styled to ANSI with glamour. One renderer per width is cached;
// rendering is pure — same input + width yields identical output.
package preview

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

var (
	mu        sync.Mutex
	renderers = map[int]*glamour.TermRenderer{}
)

// Render converts markdown to a styled ANSI string wrapped at width.
func Render(markdown string, width int) (string, error) {
	if strings.TrimSpace(markdown) == "" {
		return "", nil
	}
	r, err := rendererFor(width)
	if err != nil {
		return "", err
	}
	out, err := r.Render(markdown)
	if err != nil {
		return "", fmt.Errorf("preview: render: %w", err)
	}
	return out, nil
}

func rendererFor(width int) (*glamour.TermRenderer, error) {
	if width < 20 {
		width = 20
	}
	if width > 200 {
		width = 200 // keep line lengths readable on huge screens
	}
	mu.Lock()
	defer mu.Unlock()
	if r, ok := renderers[width]; ok {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, fmt.Errorf("preview: renderer: %w", err)
	}
	renderers[width] = r
	return r, nil
}

// IsMarkdown reports whether a path should get the preview treatment.
func IsMarkdown(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range []string{".md", ".markdown", ".mdown"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
