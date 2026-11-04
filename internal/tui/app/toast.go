package app

import (
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// toaster is the toast seam (F-093): a surface raises a short outcome
// worth a moment (a task shipped); the shell shows it top-right, slides
// it in, and clears it after toastTTL. Surfaces without it raise none.
type toaster interface {
	TakeToast() (kit.Toast, bool)
}

const (
	toastTTL      = 4 * time.Second
	toastFrames   = 4 // slide-in steps (motion on)
	toastInterval = 40 * time.Millisecond
)

type toastFrameMsg struct{ seq int }
type toastExpireMsg struct{ seq int }

// pollToasts takes the newest toast any surface raised and arms its
// slide-in and expiry ticks.
func (a *App) pollToasts() tea.Cmd {
	got := a.queuedToast
	a.queuedToast = nil
	for _, s := range a.surfaces {
		if t, ok := s.(toaster); ok {
			if tt, ok := t.TakeToast(); ok {
				got = &tt
			}
		}
	}
	if got == nil {
		return nil
	}
	notify := a.notifyCmd(got)
	a.toastSeq++
	a.toast, a.toastFrame = got, toastFrames
	seq := a.toastSeq
	expire := tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpireMsg{seq} })
	if !theme.Motion {
		return tea.Batch(expire, notify)
	}
	a.toastFrame = 1
	return tea.Batch(expire, notify, tea.Tick(toastInterval, func(time.Time) tea.Msg { return toastFrameMsg{seq} }))
}

// notifyCmd is the OSC 9 desktop notification for a toast: only while the
// terminal is blurred (F-117) and the setting is on (F-139); nil otherwise.
// iTerm2, Ghostty, WezTerm and kitty show it as a notification.
func (a *App) notifyCmd(t *kit.Toast) tea.Cmd {
	if !a.blurred || !theme.Notify {
		return nil
	}
	return tea.Raw(osc9(t.Title, t.Detail))
}

// osc9 is a desktop-notification escape: "dhi: title — detail", with
// control characters dropped so the text cannot end the sequence early.
func osc9(title, detail string) string {
	text := "dhi: " + title
	if detail != "" {
		text += " — " + detail
	}
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, text)
	return "\x1b]9;" + clean + "\x07"
}

// toastMsg advances or clears the toast; false = not a toast message.
func (a *App) toastMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case toastFrameMsg:
		if m.seq != a.toastSeq || a.toast == nil || a.toastFrame >= toastFrames {
			return nil, true
		}
		a.toastFrame++
		seq := m.seq
		return tea.Tick(toastInterval, func(time.Time) tea.Msg { return toastFrameMsg{seq} }), true
	case toastExpireMsg:
		if m.seq == a.toastSeq {
			a.toast = nil
		}
		return nil, true
	}
	return nil, false
}

// overlayToast floats the toast top-right, under the tab bar.
func (a *App) overlayToast(out string) string {
	if a.toast == nil || a.width < 20 {
		return out
	}
	box := a.toast.View(min(a.width-4, 72), float64(a.toastFrame)/float64(toastFrames))
	return kit.OverlayAt(strings.Split(out, "\n"), box, a.width, a.height, a.width-ansi.Width(box)-2, 2)
}
