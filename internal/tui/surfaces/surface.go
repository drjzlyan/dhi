// Package surfaces defines the contract every DHI surface (workspace pane)
// implements. The app shell routes messages and screen real estate; surfaces
// own their internal state and rendering.
package surfaces

import (
	"charm.land/bubbletea/v2"
)

// Meta identifies a surface in the registry and tab bar.
type Meta struct {
	ID    string // stable id ("home", "editor", …)
	Title string // human label shown in tabs and statusline
}

// Surface is one full-screen workspace of the IDE. Implementations are
// mutable values used through pointers.
type Surface interface {
	Meta() Meta

	// Init runs once when the surface is registered; may return a Cmd.
	Init() tea.Cmd

	// Resize receives the available viewport size for this surface.
	Resize(width, height int)

	// Update handles non-key messages routed to the active surface.
	Update(msg tea.Msg) tea.Cmd

	// HandleKey receives keystrokes not consumed by global bindings.
	// Returns true when the key was consumed.
	HandleKey(key string) bool

	// View renders the surface body within the last Resize dimensions.
	View() string
}

// Command is one palette entry a surface contributes (F-041). Run may
// return a tea.Cmd; it executes on the UI loop.
type Command struct {
	Group string // short dim prefix, usually the surface or area
	Title string
	Hint  string // a key or short note shown right-aligned
	Run   func() tea.Cmd
}

// CommandProvider is the palette seam: the active surface lists what it
// can do right now (the palette is context-aware — it reflects the
// surface and mode the user is in). Surfaces without it contribute
// nothing beyond the shell's global commands.
type CommandProvider interface {
	Commands() []Command
}

// InputCapturer (F-041) is the text-input seam: a surface that is
// collecting free text right now — an insert-mode buffer, a form field, a
// composer, a filter, a terminal — reports true. The shell then leaves
// every printable key, tab and "?" to it; only ctrl-chords (quit, the
// command palette) stay global. Without it the shell would turn the "2"
// of "2+2" into a view switch and the "?" of a question into the help
// overlay.
type InputCapturer interface {
	CapturesInput() bool
}

// CtrlCTaker is implemented by a surface that hosts a terminal: while it
// reports true, ctrl+c goes to the running program instead of quitting
// DHI (ctrl+q still quits).
type CtrlCTaker interface {
	TakesCtrlC() bool
}

// CmdSource lets a surface start async work from a key: HandleKey cannot
// return a tea.Cmd, so the surface queues one and the shell drains it
// right after the key (nil = nothing queued). Without it, work started
// from a key (the editor's streaming search) would never be pumped.
type CmdSource interface {
	TakeCmd() tea.Cmd
}

// Emitter (F-051) is the action-report seam: the shell hands a surface a
// function and the surface calls it, on the UI goroutine only, when the user
// does something a tutorial can wait for ("editor.save", "task.created", …;
// the names live in internal/tutorial). A surface without it simply reports
// nothing. The function is never nil once SetEmitter ran.
type Emitter interface {
	SetEmitter(emit func(event string))
}

// Mouse seams (F-026 P2): narrow interface assertions satisfied by
// individual surfaces — the Surface contract is untouched, and surfaces
// without them degrade (no mouse behavior, never a crash). The shell
// routes MouseWheelMsg/MouseClickMsg to the active surface with
// body-local coordinates (row 0 = tab bar is consumed by the shell).
//
//   - wheelHandler: Wheel(dy int) bool — dy = -1 wheel-up / +1 wheel-
//     down; true when the surface scrolled something. Surfaces route
//     wheel events to their focused pane's scroll window (kit.Scroller).
//
//   - clickHandler: Click(x, y int) bool — body-local column/row; true
//     when the surface acted (rail rows, lists, lanes, diff lines).
