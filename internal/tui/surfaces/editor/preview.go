package editor

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/drjzlyan/dhi/internal/preview"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// previewView renders the active markdown buffer, re-rendering only when
// its content changed (edit → preview updates live).
func (m *Model) previewView() string {
	e := m.active()
	text := e.Buffer().Text()

	sum := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(sum[:8]) + "|" + itoa(m.previewWidth())
	if key != m.previewKey {
		out, err := preview.Render(text, m.previewWidth()-2)
		if err != nil {
			out = theme.DangerText().Render(err.Error())
		}
		m.previewDoc = out
		m.previewKey = key
	}
	return m.previewDoc
}

func (m *Model) previewWidth() int { return maxInt(m.width-railWidth-1, 24) }
