package editor

import (
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// diskPoll is how often open buffers are compared with their files
// (F-115). The save-time check is the real guard; this only reloads a
// clean buffer early and warns a dirty one before the user saves.
const diskPoll = 2 * time.Second

type diskTickMsg struct{}

func diskTick() tea.Cmd {
	return tea.Tick(diskPoll, func(time.Time) tea.Msg { return diskTickMsg{} })
}

// checkDisk reloads clean buffers whose file changed on disk and warns,
// once per change, on dirty ones.
func (m *Model) checkDisk() {
	for _, t := range m.bufs {
		if t.ed == nil {
			continue
		}
		b := t.ed.Buffer()
		if !b.ChangedOnDisk() {
			t.diskWarned = false
			continue
		}
		if t.diskWarned {
			continue
		}
		if b.Dirty() {
			t.ed.SetMessage("changed on disk — :e! loads it (drops your edits), :w! keeps yours")
			t.diskWarned = true
			continue
		}
		if err := t.ed.Reload(); err != nil {
			t.ed.SetMessage("gone from disk — :w writes it again")
			t.diskWarned = true
			continue
		}
		t.ed.TakeReloaded()
		t.ed.SetMessage("reloaded — the file changed on disk")
		m.lspResync(t)
	}
}

// lspResync sends a tab's whole text to its language server after the
// buffer was replaced from disk.
func (m *Model) lspResync(t *bufTab) {
	if m.lspMgr == nil {
		return
	}
	l, _, ok := m.langs.For(t.path)
	if !ok {
		return
	}
	if c := m.lspMgr.ClientFor(l.ID); c != nil {
		text := t.ed.Buffer().Text()
		_ = c.DidChange(t.path, text)
		m.lspSent[t.vp] = text
	}
}

// Unsaved implements surfaces.Unsaved: the open buffers with edits.
func (m *Model) Unsaved() []string {
	var out []string
	for _, t := range m.bufs {
		if t.ed != nil && t.ed.Buffer().Dirty() {
			out = append(out, t.vp)
		}
	}
	return out
}

// SaveAll implements surfaces.Unsaved: saves every dirty buffer, keeping
// on after a failure, and names each buffer it could not save.
func (m *Model) SaveAll() error {
	var errs []error
	for _, t := range m.bufs {
		if t.ed == nil || !t.ed.Buffer().Dirty() {
			continue
		}
		if err := t.ed.Save(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.vp, err))
		}
	}
	return errors.Join(errs...)
}
