// Package gatechain runs several boot gates in order behind the shell's
// single gate slot (F-043): a blocked boot, the repairs, then the setup
// wizard. A later gate starts only when the one before it finishes.
package gatechain

import (
	"charm.land/bubbletea/v2"
)

// Gate mirrors app.Gate; kept local so gate surfaces never import the shell.
type Gate interface {
	Init() tea.Cmd
	Resize(width, height int)
	Update(tea.Msg) tea.Cmd
	HandleKey(key string) bool
	View() string
	Finished() bool
}

type taker interface{ TakeCmd() tea.Cmd }

// Chain is a Gate over an ordered list of gates.
type Chain struct {
	gates         []Gate
	cur           int
	width, height int
	started       bool
	pending       tea.Cmd
}

// New chains gates; nil entries are dropped.
func New(gates ...Gate) *Chain {
	c := &Chain{}
	for _, g := range gates {
		if g != nil {
			c.gates = append(c.gates, g)
		}
	}
	return c
}

// Len reports how many gates the chain holds.
func (c *Chain) Len() int { return len(c.gates) }

func (c *Chain) Finished() bool { return c.cur >= len(c.gates) }

func (c *Chain) Resize(w, h int) {
	c.width, c.height = w, h
	for _, g := range c.gates {
		g.Resize(w, h)
	}
}

// Init starts the first gate that is not already finished.
func (c *Chain) Init() tea.Cmd {
	c.started = true
	return c.startCurrent()
}

// startCurrent skips gates that finished on construction and Inits the
// first live one.
func (c *Chain) startCurrent() tea.Cmd {
	for c.cur < len(c.gates) {
		g := c.gates[c.cur]
		if g.Finished() {
			c.cur++
			continue
		}
		g.Resize(c.width, c.height)
		return g.Init()
	}
	return nil
}

// settle advances past a gate that has just finished and returns the next
// gate's Init command.
func (c *Chain) settle() tea.Cmd {
	if c.Finished() || !c.gates[c.cur].Finished() {
		return nil
	}
	c.cur++
	return c.startCurrent()
}

func (c *Chain) Update(msg tea.Msg) tea.Cmd {
	if c.Finished() {
		return nil
	}
	cmd := c.gates[c.cur].Update(msg)
	return tea.Batch(cmd, c.settle())
}

func (c *Chain) HandleKey(key string) bool {
	if c.Finished() {
		return false
	}
	g := c.gates[c.cur]
	handled := g.HandleKey(key)
	var own tea.Cmd
	if t, ok := g.(taker); ok {
		own = t.TakeCmd()
	}
	c.pending = tea.Batch(c.pending, own, c.settle())
	return handled
}

// TakeCmd drains what HandleKey queued (the shell calls it after a key).
func (c *Chain) TakeCmd() tea.Cmd {
	cmd := c.pending
	c.pending = nil
	return cmd
}

func (c *Chain) View() string {
	if c.Finished() {
		return ""
	}
	return c.gates[c.cur].View()
}

// Hints forwards the current gate's status-row keys (ADR-0040); a gate
// without them shows none.
func (c *Chain) Hints() (string, []string) {
	if c.Finished() {
		return "", nil
	}
	if hp, ok := c.gates[c.cur].(interface{ Hints() (string, []string) }); ok {
		return hp.Hints()
	}
	return "", nil
}

// NeedsRelaunch is true when any chained gate asks for a restart.
func (c *Chain) NeedsRelaunch() bool {
	for _, g := range c.gates {
		if r, ok := g.(interface{ NeedsRelaunch() bool }); ok && r.NeedsRelaunch() {
			return true
		}
	}
	return false
}
