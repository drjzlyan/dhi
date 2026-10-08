package editor

// syntax.go colorizes editor buffers (F-026 P4, ADR-0015) through the
// shared tokenizer in internal/tui/syntax. The whole-buffer lex caches
// on the buffer's mutation sequence (textbuf.Buffer.Seq); cursor/selection
// lines render plain so the rune-level inversion stays exact.

import (
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/syntax"
)

// highlighter caches one buffer's styled lines between edits.
type highlighter struct {
	path  string
	seq   uint64
	done  bool // seq 0 is a valid cache state — done distinguishes fresh
	lines []string
}

// styled reports the colored render for line i ("" = no styling).
func (h *highlighter) styled(i int) string {
	if i < 0 || i >= len(h.lines) {
		return ""
	}
	return h.lines[i]
}

// refresh re-lexes when the buffer moved on from the cached content.
// A nil result stays nil-safe: unlexable buffers render plain, never
// fake colored (F-011).
func (h *highlighter) refresh(b *textbuf.Buffer) {
	if b == nil {
		return
	}
	if h.done && h.seq == b.Seq() {
		return
	}
	h.done = true
	h.seq = b.Seq()
	h.lines = make([]string, b.LineCount())
	for i, l := range syntax.Lex(h.path, b.Text()) {
		if i >= len(h.lines) {
			break
		}
		h.lines[i] = syntax.Render(l)
	}
}
