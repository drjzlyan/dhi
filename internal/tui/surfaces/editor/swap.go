package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/swapfile"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Hot exit and swap recovery (F-161). While a buffer has unsaved changes
// its text is written, swapDelay after the last change, to
// .dhi/tmp/swap/<hash>.swp (per user, gitignored). The swap goes away on
// save, on close and when the buffer is clean again. Opening a file whose
// swap belongs to a dead DHI offers recover / compare / discard; one that
// belongs to a live DHI only warns (no locking). The quit guard's "keep for
// next time" rewrites the swaps with hot: true; with session restore on,
// those tabs reopen with their text and no card.

const (
	// swapDelay is how long a buffer must stay unchanged before its swap
	// is written, so typing never waits on the disk.
	swapDelay = 2 * time.Second
	// swapMaxBytes is the largest buffer that is swapped at all.
	swapMaxBytes = 4 << 20
	swapRows     = 12 // rows of the compare window
)

// WithProcess sets the process id written into swaps and the check for
// whether another id is still a running DHI (tests; default is this
// process and swapfile.PIDAlive).
func WithProcess(pid int, alive func(int) bool) Option {
	return func(m *Model) { m.pid, m.pidAlive = pid, alive }
}

// swapCard is the recovery card for one tab.
type swapCard struct {
	tab         *bufTab
	hdr         swapfile.Header
	text        string
	diskChanged bool
	compare     []string // unified lines disk -> swap; nil = the choice
	scroll      int
}

func (m *Model) swapStore() *swapfile.Store {
	if m.ws == nil {
		return nil
	}
	if m.swaps == nil {
		m.swaps = swapfile.New(filepath.Join(m.ws.Root, workspace.DHIDir, "tmp", "swap"))
	}
	return m.swaps
}

func (m *Model) ownPID() int {
	if m.pid != 0 {
		return m.pid
	}
	return os.Getpid()
}

func (m *Model) isAlive(pid int) bool {
	if m.pidAlive != nil {
		return m.pidAlive(pid)
	}
	return swapfile.PIDAlive(pid)
}

// swappable is whether a tab's edits get a swap: a real file in the
// workspace, not DHI's own (the commit message), not held off by a card.
func (m *Model) swappable(t *bufTab) bool {
	return m.ws != nil && t.ed != nil && t.path != "" && !t.noSwap && t.edit == nil &&
		!within(t.path, filepath.Join(m.ws.Root, workspace.DHIDir))
}

// noteBase records the disk text a tab was loaded from or last saved as,
// which every swap names so a later change on disk can be told apart.
func (m *Model) noteBase(t *bufTab) {
	t.baseHash = swapfile.HashText(t.ed.Buffer().Text())
	t.baseMTime = 0
	if fi, err := os.Stat(t.path); err == nil {
		t.baseMTime = fi.ModTime().UnixNano()
	}
}

func (m *Model) swapHeader(t *bufTab, hot bool) swapfile.Header {
	return swapfile.Header{Path: t.path, BaseHash: t.baseHash, BaseMTime: t.baseMTime,
		At: m.clock(), PID: m.ownPID(), Hot: hot}
}

// swapTick writes the swap of every buffer that has been dirty and
// unchanged for swapDelay, and drops the swap of one that is clean again.
func (m *Model) swapTick() {
	st := m.swapStore()
	if st == nil {
		return
	}
	now := m.clock()
	for _, t := range m.bufs {
		if !m.swappable(t) {
			continue
		}
		if !t.ed.Buffer().Dirty() {
			if t.swapWritten {
				st.Remove(t.path)
			}
			if t.swapWritten || t.swapKnown {
				m.noteBase(t) // clean again: it matches the disk (saved, undone, or reloaded)
			}
			t.swapWritten, t.swapKnown = false, false
			continue
		}
		text := t.ed.Buffer().Text()
		if !t.swapKnown || text != t.swapSeen {
			t.swapSeen, t.swapAt, t.swapKnown, t.swapPending = text, now, true, true
			continue
		}
		if t.swapPending && now.Sub(t.swapAt) >= swapDelay && len(text) <= swapMaxBytes {
			st.Write(m.swapHeader(t, false), text)
			t.swapPending, t.swapWritten = false, true
		}
	}
}

// dropSwap removes a tab's swap: saved, closed or discarded.
func (m *Model) dropSwap(t *bufTab) {
	if st := m.swapStore(); st != nil && t != nil && t.path != "" {
		st.Remove(t.path)
		t.swapWritten, t.swapKnown, t.swapPending = false, false, false
	}
}

// savedSwap runs after a save: the swap is stale and the saved text is the
// new base.
func (m *Model) savedSwap(path string) {
	if t := m.tabFor(path); t != nil {
		m.dropSwap(t)
		m.noteBase(t)
	}
}

// KeepForNextTime implements surfaces.HotExit: it writes a hot swap for
// every dirty buffer so the next launch brings the edits back.
func (m *Model) KeepForNextTime() error {
	m.closeRebaseServer() // F-165: DHI is leaving; the shim's folder goes with it
	st := m.swapStore()
	if st == nil {
		return fmt.Errorf("hot exit needs a workspace")
	}
	var lost []string
	for _, t := range m.bufs {
		if t.ed == nil || !t.ed.Buffer().Dirty() {
			continue
		}
		if m.ws != nil && t.path == m.commitPath() {
			continue // a half-written commit message is not worth a swap
		}
		if t.path == "" || t.swapHeld() || !m.swappable(t) {
			lost = append(lost, t.vp)
			continue
		}
		text := t.ed.Buffer().Text()
		if len(text) > swapMaxBytes {
			lost = append(lost, t.vp)
			continue
		}
		if err := st.WriteSync(m.swapHeader(t, true), text); err != nil {
			return fmt.Errorf("%s: %w", t.vp, err)
		}
		t.swapWritten = true
	}
	if len(lost) > 0 {
		return fmt.Errorf("cannot keep %s — save it or quit without saving", strings.Join(lost, ", "))
	}
	return nil
}

// DiscardSwaps implements surfaces.HotExit: quitting without saving
// drops the swaps along with the edits.
func (m *Model) DiscardSwaps() {
	for _, t := range m.bufs {
		if t.ed != nil && t.ed.Buffer().Dirty() && !t.swapHeld() {
			m.dropSwap(t)
		}
	}
}

// swapHeld is a tab whose existing swap must not be touched: another DHI
// owns it, or its card is still undecided.
func (t *bufTab) swapHeld() bool { return t.noSwap }

// swapOnOpen looks for a swap when a file opens.
func (m *Model) swapOnOpen(t *bufTab) {
	m.noteBase(t)
	st := m.swapStore()
	if st == nil || !m.swappable(t) {
		return
	}
	h, text, ok := st.Read(t.path)
	if !ok {
		return
	}
	cur := t.ed.Buffer().Text()
	switch {
	case h.PID == m.ownPID():
		st.Remove(t.path) // ours, from before this tab was closed
		return
	case m.isAlive(h.PID):
		t.noSwap = true
		t.ed.SetMessage(fmt.Sprintf("open in another DHI (pid %d), which has unsaved changes here — your edits will not be kept in a swap", h.PID))
		return
	case text == cur:
		st.Remove(t.path) // nothing to recover
		return
	}
	changed := h.BaseHash != t.baseHash
	if h.Hot && m.restoring && !changed {
		m.applySwap(t, text, "restored your unsaved changes from last time")
		return
	}
	t.noSwap = true // until the card is answered, the old swap is the only copy
	m.swapCard = &swapCard{tab: t, hdr: h, text: text, diskChanged: changed}
}

// applySwap puts swap text into the buffer as one undo step from the disk
// text; the buffer is dirty until :w.
func (m *Model) applySwap(t *bufTab, text, msg string) {
	if !t.ed.Buffer().SetText(text) {
		return
	}
	m.lspResync(t)
	t.ed.SetMessage(msg + " — :w to save, u to undo")
}

func (m *Model) swapKey(key string) bool {
	c := m.swapCard
	if c.compare != nil {
		switch key {
		case "esc", "q":
			c.compare = nil
		case "j", "down":
			c.scroll = min(c.scroll+1, max(len(c.compare)-swapRows, 0))
		case "k", "up":
			c.scroll = max(c.scroll-1, 0)
		case "pgdown", "ctrl+d":
			c.scroll = min(c.scroll+swapRows, max(len(c.compare)-swapRows, 0))
		case "pgup", "ctrl+u":
			c.scroll = max(c.scroll-swapRows, 0)
		case "r":
			m.swapRecover()
		case "d":
			m.swapDiscard()
		}
		return true
	}
	switch key {
	case "r", "enter":
		m.swapRecover()
	case "c":
		c.compare = unifiedLines(strings.Split(c.tab.ed.Buffer().Text(), "\n"), strings.Split(c.text, "\n"))
		if len(c.compare) == 0 {
			c.compare = []string{"(identical to the file)"}
		}
		c.scroll = 0
	case "d":
		m.swapDiscard()
	case "esc":
		// Decide later: the swap stays and no new one is written for this
		// tab, so reopening the file asks again.
		c.tab.ed.SetMessage("swap kept — reopen the file to recover or discard it")
		m.swapCard = nil
	}
	return true
}

func (m *Model) swapRecover() {
	c := m.swapCard
	m.swapCard = nil
	c.tab.noSwap = false
	if !m.tabOpen(c.tab) {
		return
	}
	m.applySwap(c.tab, c.text, "recovered the unsaved changes from "+snapWhen(c.hdr.At, m.clock()))
	if !c.tab.ed.Buffer().Dirty() {
		m.dropSwap(c.tab)
	}
}

func (m *Model) swapDiscard() {
	c := m.swapCard
	m.swapCard = nil
	c.tab.noSwap = false
	m.dropSwap(c.tab)
	if m.tabOpen(c.tab) {
		c.tab.ed.SetMessage("swap discarded")
	}
}

func (m *Model) swapOverlay(base string) string {
	c := m.swapCard
	name := m.relToWorkspace(c.tab.path)
	var lines []string
	title := "Unsaved changes found · " + filepath.Base(name)
	if c.compare != nil {
		title += " · file → swap"
		end := min(c.scroll+swapRows, len(c.compare))
		for _, l := range c.compare[c.scroll:end] {
			lines = append(lines, styleUnified(expandTabs(l)))
		}
		lines = append(lines, "", theme.Hint().Render("r recover · d discard · ↑/↓ scroll · esc back"))
	} else {
		lines = append(lines,
			theme.TextStyle().Render(name+" was left with unsaved changes ("+snapWhen(c.hdr.At, m.clock())+")."),
			theme.TextMuted().Render("DHI (pid "+fmt.Sprint(c.hdr.PID)+") is no longer running."))
		if c.diskChanged {
			lines = append(lines, "", theme.WarningText().Render("The file also changed on disk since those edits began."))
		}
		lines = append(lines, "",
			theme.Keycap().Render("r")+theme.TextMuted().Render(" recover   ")+
				theme.Keycap().Render("c")+theme.TextMuted().Render(" compare   ")+
				theme.Keycap().Render("d")+theme.TextMuted().Render(" discard   ")+
				theme.Keycap().Render("esc")+theme.TextMuted().Render(" decide later"))
	}
	md := kit.Modal{Title: title, Lines: lines, Width: min(m.width-4, 100)}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}
