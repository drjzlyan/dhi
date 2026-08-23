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
