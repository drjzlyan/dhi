package editor

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// Autosave (F-142) saves through the same Editor.Save as :w, so the
// changed-on-disk check, format-on-save and the LSP didSave all apply.
const (
	// autosaveDelay is how long the editor must see no keys before
	// "after delay" saves, so nothing is written mid-typing.
	autosaveDelay = time.Second
	// autosavePoll is how often the delay is checked.
	autosavePoll = 500 * time.Millisecond
)

type autosaveTickMsg struct{}

func autosaveTick() tea.Cmd {
	return tea.Tick(autosavePoll, func(time.Time) tea.Msg { return autosaveTickMsg{} })
}

// WithClock replaces the wall clock (tests).
func WithClock(fn func() time.Time) Option {
	return func(m *Model) { m.now = fn }
}

func (m *Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *Model) autosaveMode() string { return m.editorPrefs().Autosave }

// focusedTab is the tab the buffer pane shows, or nil.
func (m *Model) focusedTab() *bufTab {
	if m.activeTab < len(m.bufs) {
		return m.bufs[m.activeTab]
	}
	return nil
}

// autosaveDelayed saves every modified buffer once the editor has seen
// no key for autosaveDelay.
func (m *Model) autosaveDelayed() {
	if m.autosaveMode() != settings.AutosaveDelay || m.clock().Sub(m.lastKey) < autosaveDelay {
		return
	}
	for _, t := range m.bufs {
		m.autosaveTab(t)
	}
}

// autosaveFocusChange saves the buffer a key just moved focus away from
// (another tab or pane, or back to the tree).
func (m *Model) autosaveFocusChange(prev *bufTab, prevFocus bool) {
	if m.autosaveMode() != settings.AutosaveFocus || prev == nil || !prevFocus {
		return
	}
	if (m.focusedTab() != prev || !m.bufFocus) && m.tabOpen(prev) {
		m.autosaveTab(prev)
	}
}

func (m *Model) tabOpen(t *bufTab) bool {
	for _, o := range m.bufs {
		if o == t {
			return true
		}
	}
	return false
}

// autosaveTab saves one modified file buffer. Untitled buffers and the
// commit message are never saved. A file that changed on disk is never
// overwritten: autosave stops for that buffer until it is clean again.
func (m *Model) autosaveTab(t *bufTab) {
	if t.ed == nil {
		return
	}
	if !t.ed.Buffer().Dirty() {
		t.noAutosave = false
		return
	}
	if t.noAutosave || t.edit != nil || t.path == "" || t.ed.Path() == "" || (m.ws != nil && (t.path == m.commitPath() || t.path == m.searchBufPath())) {
		return
	}
	err := t.ed.Save()
	switch {
	case err == nil:
	case errors.Is(err, textbuf.ErrChangedOnDisk):
		t.noAutosave = true
		t.ed.SetMessage("autosave stopped — changed on disk; :w! keeps yours, :e! loads it")
	default:
		t.noAutosave = true
		t.ed.SetMessage("autosave stopped — " + err.Error())
	}
}
