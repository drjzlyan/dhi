// Package app implements the DHI shell: tab navigation, focus routing,
// global keybindings, help overlay, and the statusline.
package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// View-transition fade-in (F-012): a surface switch renders the new body
// dimmed for a couple of short frames before settling. Message-driven like
// every other DHI animation; reduced motion skips it entirely. Cadence
// lives in theme (F-026 P1).
var (
	transitionFrames   = theme.MotionFrames
	transitionInterval = theme.MotionInterval
)

type transitionMsg struct{}

// activityTickMsg advances the presence spinner while agents work.
type activityTickMsg struct{}

// activityInterval is the spinner cadence.
const activityInterval = 120 * time.Millisecond

var spinFrames = kit.SpinnerFrames

// Gate is a full-body takeover shown before normal surfaces (first-run
// bootstrap, boot gates). While the gate is active it owns Update/View
// and receives every key via HandleKey; global keys still quit, and
// once Finished() reports true the shell resumes ordinary routing
// permanently. A gate that never finishes blocks boot (ADR-0011).
type Gate interface {
	Init() tea.Cmd
	Resize(width, height int)
	Update(tea.Msg) tea.Cmd
	HandleKey(key string) bool
	View() string
	Finished() bool
}

// App is the root Bubble Tea model.
type App struct {
	version  string
	surfaces []surfaces.Surface
	active   int
	tabs     *kit.Tabs

	gate      Gate
	gateRan   bool
	setupGate func() Gate // palette "Run setup wizard" (F-043)

	transLeft int // fade-in frames remaining for the active surface

	width, height int
	showHelp      bool
	quitting      bool

	// welcome is the one-time first-run card (F-041); nil once dismissed.
	// onWelcomeDismiss records that it was seen.
	welcome          bool
	onWelcomeDismiss func()

	// activity reports in-flight agent turns for the presence chip;
	// nil hides the chip. spin advances the spinner frame; armed marks a
	// pending activity tick (so one chain runs at a time).
	activity func() int
	spin     int
	armed    bool

	// palette is the open command palette (ctrl+p); nil when closed.
	palette *kit.Palette
}

// New wires the shell around an ordered surface registry (index i answers
// key strconv.Itoa(i+1)).
func New(version string, regs ...surfaces.Surface) *App {
	a := &App{version: version, surfaces: regs}
	pairs := make([][2]string, len(regs))
	for i, s := range regs {
		pairs[i] = [2]string{s.Meta().ID, s.Meta().Title}
	}
	a.tabs = kit.NewTabs(pairs...)
	return a
}

// Active returns the focused surface.
func (a *App) Active() surfaces.Surface { return a.surfaces[a.active] }

// SetWelcome arms the one-time welcome card, shown after any boot gate
// has released. onDismiss runs once when the user closes it (the caller
// persists "seen"). Call before Init.
func (a *App) SetWelcome(onDismiss func()) {
	a.welcome, a.onWelcomeDismiss = true, onDismiss
}

// SetActivity installs the agent-activity source behind the tab-bar
// presence chip ("⠹ 2 agents working"). Call before Init.
func (a *App) SetActivity(fn func() int) { a.activity = fn }

// activityChip renders the presence chip, "" when nothing is running.
func (a *App) activityChip() string {
	if a.activity == nil {
		return ""
	}
	n := a.activity()
	if n <= 0 {
		return ""
	}
	glyph := theme.GlyphBusy // reduced motion: a static glyph, no ticking
	if theme.Motion {
		glyph = spinFrames[a.spin%len(spinFrames)]
	}
	noun := "agent working"
	if n > 1 {
		noun = fmt.Sprintf("%d agents working", n)
	} else {
		noun = "1 " + noun
	}
	return theme.SuccessText().Render(glyph + " " + noun)
}

// activityCmd arms the spinner tick chain when agents are working and
// motion is on; at most one chain runs.
func (a *App) activityCmd() tea.Cmd {
	if a.activity == nil || a.armed || !theme.Motion || a.activity() <= 0 {
		return nil
	}
	a.armed = true
	return tea.Tick(activityInterval, func(time.Time) tea.Msg { return activityTickMsg{} })
}

// SetGate installs a first-run gate (e.g. toolchain bootstrap). Call
// before Init.
func (a *App) SetGate(g Gate) { a.gate = g }

// gateActive reports whether the gate currently owns the body.
func (a *App) gateActive() bool { return a.gate != nil && !a.gateRan }

// Init satisfies tea.Model; every surface gets its Init cmd.
func (a *App) Init() tea.Cmd {
	var cmds []tea.Cmd
	if a.gate != nil {
		cmds = append(cmds, a.gate.Init())
	}
	for _, s := range a.surfaces {
		if c := s.Init(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// EditorRequest is an agent-requested editor action routed through the
// Update loop (ADR-0023): the runtime's editor seam sends it and blocks
// on Reply. Paths are absolute; the seam resolves VPaths before sending.
type EditorRequest struct {
	Op    string // "open" | "reveal" | "apply" | "context" | "propose" | "lsp"
	Paths []string
	// Path/Old/New/All carry an "apply" edit.
	Path string
	Old  string
	New  string
	All  bool
	// Op "lsp" carries the LSP verb + position/argument.
	LSPOp string
	Line  int
	Col   int
	Arg   string
	// Op "propose" carries the suggestion note and proposing agent.
	Note  string
	From  string
	Reply chan EditorReply
}

// EditorReply is the result of an EditorRequest.
type EditorReply struct {
	Text string // "lsp" results
	Err  string
}

// handleEditorRequest performs one agent editor action on the UI loop.
func (a *App) handleEditorRequest(r EditorRequest) {
	var errStr string
	// LSP calls can block on the server, so they run off the UI loop and
	// answer on the reply channel when done (ADR-0023).
	if r.Op == "lsp" {
		go func(req EditorRequest) {
			text, err := a.lspInEditor(req)
			reply := EditorReply{Text: text}
			if err != nil {
				reply.Err = err.Error()
			}
			if req.Reply != nil {
				req.Reply <- reply
			}
		}(r)
		return
	}
	switch r.Op {
	case "open", "reveal":
		if !a.OpenInEditor(r.Paths) {
			errStr = "editor could not open the requested path(s)"
		}
	case "apply":
		errStr = a.applyInEditor(r.Path, r.Old, r.New, r.All)
	case "context", "propose", "debug":
		text, err := a.pairInEditor(r)
		if err != nil {
			errStr = err.Error()
		}
		if r.Reply != nil {
			r.Reply <- EditorReply{Text: text, Err: errStr}
		}
		return
	default:
		errStr = "unknown editor request " + r.Op
	}
	if r.Reply != nil {
		r.Reply <- EditorReply{Err: errStr}
	}
}

// pairInEditor routes a pair-programming request (F-038) to the editor
// surface: "context" reads the human's position, "propose" queues a
// suggestion for their review.
func (a *App) pairInEditor(r EditorRequest) (string, error) {
	for _, s := range a.surfaces {
		if s.Meta().ID != "editor" {
			continue
		}
		if r.Op == "debug" {
			dbg, ok := s.(interface{ DebugState() (string, error) })
			if !ok {
				return "", fmt.Errorf("editor does not expose a debugger")
			}
			return dbg.DebugState()
		}
		pr, ok := s.(interface {
			Context() (string, error)
			Propose(abs, old, new, note, from string) error
		})
		if !ok {
			return "", fmt.Errorf("editor does not support pair programming")
		}
		if r.Op == "context" {
			return pr.Context()
		}
		return "", pr.Propose(r.Path, r.Old, r.New, r.Note, r.From)
	}
	return "", fmt.Errorf("editor surface unavailable")
}

// lspInEditor routes one LSP verb to the editor surface (ADR-0023).
func (a *App) lspInEditor(r EditorRequest) (string, error) {
	for _, s := range a.surfaces {
		if s.Meta().ID != "editor" {
			continue
		}
		caller, ok := s.(interface {
			LSPCall(context.Context, string, string, int, int, string) (string, error)
		})
		if !ok {
			return "", fmt.Errorf("editor cannot serve LSP requests")
		}
		return caller.LSPCall(context.Background(), r.LSPOp, r.Path, r.Line, r.Col, r.Arg)
	}
	return "", fmt.Errorf("editor surface unavailable")
}

// Update routes messages: gate → global keys → surface keys; broadcast
// resizes.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.update(msg)
	if ac := a.activityCmd(); ac != nil {
		if cmd == nil {
			return m, ac
		}
		return m, tea.Batch(cmd, ac)
	}
	return m, cmd
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case activityTickMsg:
		a.armed = false
		a.spin++
		return a, nil // Update re-arms while agents are still working

	case EditorRequest:
		a.handleEditorRequest(msg)
		return a, nil

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		for _, s := range a.surfaces {
			s.Resize(a.bodyWidth(), a.bodyHeight())
		}
		if a.gate != nil {
			a.gate.Resize(a.bodyWidth(), a.bodyHeight())
		}
		return a, nil

	case tea.KeyPressMsg:
		key := keyString(msg)
		if a.gateActive() {
			switch key {
			case "ctrl+c", "ctrl+q":
				a.quitting = true
				return a, tea.Quit
			}
			a.gate.HandleKey(key) // the gate owns all other input
			// A key may START work (bootgate confirm → install): gates
			// cannot return commands from HandleKey, so they queue one
			// and the shell drains it here.
			var cmd tea.Cmd
			if cg, ok := a.gate.(interface{ TakeCmd() tea.Cmd }); ok {
				cmd = cg.TakeCmd()
			}
			// A key can also be the thing that finishes the gate (the
			// setup wizard's last enter); without a timer running there
			// would be no later message to notice it.
			return a, a.releaseGate(cmd)
		}
		if a.welcome {
			switch key {
			case "ctrl+c", "ctrl+q":
				a.quitting = true
				return a, tea.Quit
			case "enter", "esc", " ", "q":
				a.dismissWelcome()
			}
			return a, nil // the card owns the keyboard until dismissed
		}
		if a.palette != nil {
			return a, a.paletteKey(key)
		}
		if key == "ctrl+p" {
			a.openPalette()
			return a, nil
		}
		if cmd, handled := a.handleGlobal(key, a.activeCapturesInput()); handled {
			return a, cmd
		}
		a.Active().HandleKey(key)
		return a, nil

	case tea.MouseWheelMsg:
		if a.gateActive() {
			return a, nil
		}
		m := msg.Mouse()
		dy := -1
		if m.Button == tea.MouseWheelDown {
			dy = 1
		}
		if wh, ok := a.Active().(interface{ Wheel(dy int) bool }); ok {
			wh.Wheel(dy)
		}
		return a, nil

	case tea.MouseClickMsg:
		if a.gateActive() {
			return a, nil
		}
		m := msg.Mouse()
		return a, a.handleClick(m.X, m.Y)

	case transitionMsg:
		if a.transLeft > 0 {
			a.transLeft--
			return a, a.transitionCmd()
		}
		return a, nil

	default:
		if a.gateActive() {
			return a, a.releaseGate(a.gate.Update(msg))
		}
		return a, a.Active().Update(msg)
	}
}

// keyString is the key as surfaces see it. Bubble Tea names the space bar
// "space", which no text input would accept as a character (every one
// takes single printable runes), so spaces could not be typed in forms,
// composers or the editor's insert mode. A bare space is passed as " ";
// chords (ctrl+space) keep their names.
func keyString(msg tea.KeyPressMsg) string {
	if msg.Code == tea.KeySpace && msg.Mod == 0 {
		return " "
	}
	return msg.String()
}

// releaseGate hands the body back to the shell once the gate reports
// Finished, batching cmd with the fade-in. The handoff fades in like a
// surface switch (F-012); reduced motion keeps it a hard cut.
func (a *App) releaseGate(cmd tea.Cmd) tea.Cmd {
	if !a.gateActive() || !a.gate.Finished() {
		return cmd
	}
	a.gateRan = true
	a.startTransition()
	if c := a.transitionCmd(); c != nil {
		if cmd == nil {
			return c
		}
		return tea.Batch(cmd, c)
	}
	return cmd
}

// StartGate installs g as the active gate from a running shell (the
// palette's "Run setup wizard") and returns its Init command.
func (a *App) StartGate(g Gate) tea.Cmd {
	a.gate, a.gateRan = g, false
	a.palette = nil
	g.Resize(a.bodyWidth(), a.bodyHeight())
	return g.Init()
}

// SetSetupGate registers the factory behind the palette's "Run setup
// wizard"; nil hides the entry.
func (a *App) SetSetupGate(f func() Gate) { a.setupGate = f }

// activeCapturesInput reports whether the focused surface is collecting
// free text (surfaces.InputCapturer); such a surface owns plain keys.
func (a *App) activeCapturesInput() bool {
	c, ok := a.Active().(surfaces.InputCapturer)
	return ok && c.CapturesInput()
}

func (a *App) handleGlobal(key string, capturing bool) (tea.Cmd, bool) {
	switch key {
	case "ctrl+c", "ctrl+q":
		a.quitting = true
		return tea.Quit, true
	}
	if capturing {
		return nil, false // typing: digits, "?" and tab belong to the surface
	}
	switch key {
	case "?":
		a.showHelp = !a.showHelp
		return nil, true
	case "tab":
		a.selectSurface((a.active + 1) % len(a.surfaces))
		return a.transitionCmd(), true
	case "shift+tab":
		a.selectSurface((a.active - 1 + len(a.surfaces)) % len(a.surfaces))
		return a.transitionCmd(), true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		n, _ := strconv.Atoi(key)
		if n <= len(a.surfaces) {
			a.selectSurface(n - 1)
			return a.transitionCmd(), true
		}
	}
	return nil, false
}

func (a *App) dismissWelcome() {
	if !a.welcome {
		return
	}
	a.welcome = false
	if a.onWelcomeDismiss != nil {
		a.onWelcomeDismiss()
	}
}

// welcomeView is the first-run card: the five things worth knowing.
func (a *App) welcomeView() string {
	titles := make([]string, len(a.surfaces))
	for i, s := range a.surfaces {
		titles[i] = s.Meta().Title
	}
	rows := [][2]string{
		{fmt.Sprintf("1-%d", len(a.surfaces)), strings.Join(titles, " · ")},
		{"ctrl+p", "command palette — search every action"},
		{"?", "keyboard help for where you are"},
		{"[  ]", "switch sections inside a view"},
		{"@name", "mention an agent in any channel to hand it work"},
	}
	lines := []string{theme.Brand().Render("Welcome to DHI"),
		theme.TextDim().Render("Your virtual office — agents work beside you."), ""}
	for _, r := range rows {
		lines = append(lines, "  "+keycap(r[0])+"  "+theme.TextDim().Render(r[1]))
	}
	lines = append(lines, "", theme.Hint().Render("press enter to begin"))
	m := kit.Modal{Title: "welcome", Lines: lines, Width: 74}
	return m.View()
}

// paletteKey routes a key to the open palette; a pick runs its command.
func (a *App) paletteKey(key string) tea.Cmd {
	picked, closed := a.palette.HandleKey(key)
	if !closed {
		return nil
	}
	a.palette = nil
	if picked == nil {
		return nil
	}
	if run, ok := picked.Data.(func() tea.Cmd); ok && run != nil {
		return run()
	}
	return nil
}

// openPalette builds the context-aware command list: the shell's global
// commands first, then whatever the active surface offers right now.
func (a *App) openPalette() {
	var items []kit.PaletteItem
	add := func(group, title, hint string, run func() tea.Cmd) {
		items = append(items, kit.PaletteItem{Group: group, Title: title, Hint: hint, Data: run})
	}
	for i, s := range a.surfaces {
		i := i
		hint := ""
		if i < 9 {
			hint = strconv.Itoa(i + 1)
		}
		add("Go to", s.Meta().Title, hint, func() tea.Cmd {
			a.selectSurface(i)
			return a.transitionCmd()
		})
	}
	if cp, ok := a.Active().(surfaces.CommandProvider); ok {
		for _, c := range cp.Commands() {
			c := c
			add(c.Group, c.Title, c.Hint, c.Run)
		}
	}
	if a.setupGate != nil {
		add("Setup", "Run setup wizard", "", func() tea.Cmd { return a.StartGate(a.setupGate()) })
	}
	add("", "Show keyboard help", "?", func() tea.Cmd { a.showHelp = true; return nil })
	add("Theme", "Dark", "", func() tea.Cmd { theme.Current = theme.Dark(); return nil })
	add("Theme", "Light", "", func() tea.Cmd { theme.Current = theme.Light(); return nil })
	add("", "Quit DHI", "ctrl+c", func() tea.Cmd { a.quitting = true; return tea.Quit })
	a.palette = kit.NewPalette(items)
}

func (a *App) selectSurface(i int) {
	if a.tabs.SetActive(i) {
		a.active = i
		a.startTransition()
	}
}

// startTransition begins the fade-in on the newly active surface;
// reduced motion (F-012) leaves transLeft at zero — an instant swap.
func (a *App) startTransition() {
	if theme.Motion {
		a.transLeft = transitionFrames
	}
}

// transitionCmd arms the next fade frame while one is in flight.
func (a *App) transitionCmd() tea.Cmd {
	if a.transLeft <= 0 {
		return nil
	}
	return tea.Tick(transitionInterval, func(time.Time) tea.Msg { return transitionMsg{} })
}

// OpenInEditor hands file paths to the editor surface: buffers open
// preloaded and focus switches across. It is the F-005 "open in editor"
// completion seam; returns false when no editor surface accepts them.
func (a *App) OpenInEditor(paths []string) bool {
	for i, s := range a.surfaces {
		if s.Meta().ID != "editor" {
			continue
		}
		op, ok := s.(interface {
			OpenPaths(...string) int
		})
		if !ok {
			return false
		}
		opened := op.OpenPaths(paths...)
		if opened > 0 {
			a.selectSurface(i)
		}
		return opened > 0
	}
	return false
}

// applyInEditor routes an agent edit to the editor surface (ADR-0023);
// "" means success, else a named error.
func (a *App) applyInEditor(abs, old, new string, all bool) string {
	for _, s := range a.surfaces {
		if s.Meta().ID != "editor" {
			continue
		}
		ap, ok := s.(interface {
			ApplyReplace(string, string, string, bool) error
		})
		if !ok {
			return "editor cannot apply edits"
		}
		if err := ap.ApplyReplace(abs, old, new, all); err != nil {
			return err.Error()
		}
		return ""
	}
	return "editor surface unavailable"
}

// FocusEditorChat opens the editor's chat sidebar focused (the F-016
// approval jump). Returns false when no editor surface exposes a chat.
func (a *App) FocusEditorChat() bool {
	for i, s := range a.surfaces {
		if s.Meta().ID != "editor" {
			continue
		}
		f, ok := s.(interface{ FocusChat() bool })
		if !ok {
			return false
		}
		if f.FocusChat() {
			a.selectSurface(i)
			return true
		}
	}
	return false
}

// SelectReviewer jumps the reviewer surface to one review card (the
// F-016 in-review jump). Returns false when unknown or unavailable.
func (a *App) SelectReviewer(id string) bool {
	for i, s := range a.surfaces {
		if s.Meta().ID != "reviewer" {
			continue
		}
		sel, ok := s.(interface {
			SelectReview(string) bool
		})
		if !ok {
			return false
		}
		if sel.SelectReview(id) {
			a.selectSurface(i)
			return true
		}
	}
	return false
}

// SelectProposal jumps the ideator surface to one pending proposal (the
// F-033 inbox jump). Returns false when unknown or unavailable.
func (a *App) SelectProposal(id int) bool {
	for i, s := range a.surfaces {
		if s.Meta().ID != "ideator" {
			continue
		}
		sel, ok := s.(interface {
			SelectProposal(int) bool
		})
		if !ok {
			return false
		}
		if sel.SelectProposal(id) {
			a.selectSurface(i)
			return true
		}
	}
	return false
}

// attentionCount sums the open attention items across surfaces (F-016
// statusline !N segment; recomputed on demand — items resolve out of the
// count the frame their home surface flips them).
func (a *App) attentionCount() int {
	n := 0
	for _, s := range a.surfaces {
		if c, ok := s.(interface{ AttentionCount() int }); ok {
			n += c.AttentionCount()
		}
	}
	return n
}

func (a *App) bodyWidth() int { return a.width }
func (a *App) bodyHeight() int {
	h := a.height - theme.Current.HeightTab - theme.Current.HeightState
	if h < 3 {
		h = 3
	}
	return h
}

// View composes tab bar + active surface + optional help overlay + statusline.
func (a *App) View() tea.View {
	v := tea.NewView(a.compose())
	v.AltScreen = true
	v.BackgroundColor = theme.Current.Bg
	// Click + wheel events reach the shell as MouseClickMsg/MouseWheelMsg
	// (F-026 P2); the shell routes them to the tab bar and the active
	// surface via narrow seams.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// handleClick routes body-local clicks: the help overlay closes on any
// click, row 0 hits the tab bar, else the active surface decides via
// its Click seam (nil-safe — surfaces without mouse behavior ignore it).
func (a *App) handleClick(x, y int) tea.Cmd {
	if a.welcome {
		a.dismissWelcome()
		return nil
	}
	if a.palette != nil {
		a.palette = nil // a click outside dismisses the palette
		return nil
	}
	if a.showHelp {
		a.showHelp = false
		return nil
	}
	if y == 0 {
		if i, ok := a.tabs.Hit(x); ok {
			a.selectSurface(i)
		}
		return nil
	}
	if y >= a.height-1 { // statusline: no action
		return nil
	}
	if ch, ok := a.Active().(interface{ Click(x, y int) bool }); ok {
		ch.Click(x, y-1)
	}
	return nil
}

func (a *App) compose() string {
	a.tabs.Width = a.width
	a.tabs.Right = a.activityChip()
	bar := a.tabs.View()

	statusLine := a.buildStatus().View()

	if a.gateActive() {
		return bar + "\n" + a.gate.View() + "\n" + statusLine
	}

	body := a.Active().View()
	if a.transLeft > 0 { // fade-in frames (F-012); content unchanged
		body = theme.Faint(body)
	}
	out := bar + "\n" + body + "\n" + statusLine
	if a.welcome && !a.gateActive() {
		out = kit.Overlay(strings.Split(out, "\n"), a.welcomeView(), a.width, a.height)
	} else if a.palette != nil {
		box := a.palette.View(min(70, max(a.width-4, 40)))
		out = kit.Overlay(strings.Split(out, "\n"), box, a.width, a.height)
	} else if a.showHelp {
		// The help dialog overlays the composed view over a dimmed
		// backdrop (F-024) — the surface stays visible beneath it.
		box := a.helpView()
		over := kit.Overlay(strings.Split(out, "\n"), box, a.width, a.height)
		out = over
	}
	return out
}

// Surfaces may expose their active zone + mode for the statusline
// (F-025 Part C). Narrow interface assertions — the Surface contract
// is untouched, and surfaces without them degrade to the neutral base
// line. Keymaps are NOT repeated here: they live once, on the pane's
// chrome HintBar.
type statusContext interface{ StatusContext() (zone, mode string) }

// buildStatus composes the statusline fresh every frame so mode and
// zone changes render live without a surface switch.
func (a *App) buildStatus() *kit.StatusLine {
	sl := kit.DefaultStatusLine(a.Active().Meta().Title)
	// Hints are derived from the live state: the view count follows the
	// registry, and while a surface is taking text the digit/tab/? keys
	// are not offered (they would be typed, not obeyed).
	if a.activeCapturesInput() {
		sl.Hints = []string{"^p palette", "^c quit"}
	} else {
		sl.Hints = []string{fmt.Sprintf("1-%d views", len(a.surfaces)), "tab next", "^p palette", "? help", "^c quit"}
	}
	if sc, ok := a.Active().(statusContext); ok {
		zone, mode := sc.StatusContext()
		left := make([]kit.StatusSegment, 0, 3)
		if mode != "" {
			left = append(left, kit.ModeChip(mode))
		}
		base := make([]kit.StatusSegment, len(sl.Left))
		copy(base, sl.Left)
		if zone != "" {
			base = append(base, kit.StatusSegment{
				Text:  " " + theme.GlyphChevron + " " + zone,
				Style: theme.TextDim(),
			})
		}
		left = append(left, base...)
		sl.Left = left
	}
	if n := a.attentionCount(); n > 0 {
		sl.Left = append([]kit.StatusSegment{{
			Text:  fmt.Sprintf(" !%d ", n),
			Style: theme.DangerText(),
		}}, sl.Left...)
	}
	sl.Center = ""
	sl.Width = a.width
	return sl
}

// helpProvider is the contextual-help seam (F-026 P7): surfaces with
// it contribute their live key sections (the same wording as their
// chrome HintBar); surfaces without it get globals only.
type helpProvider interface {
	HelpSections() [][2]string // (keys, description) pairs, current context
}

func (a *App) helpView() string {
	n := len(a.surfaces)
	rows := [][2]string{
		{fmt.Sprintf("1-%d", n), "jump between views"},
		{"tab / shift+tab", "cycle views"},
		{"ctrl+p", "command palette (search every action)"},
		{"?", "toggle this help"},
		{"ctrl+c", "quit DHI"},
	}
	lines := []string{theme.Brand().Render("DHI — global keys"), ""}
	for _, r := range rows {
		lines = append(lines, "  "+keycap(r[0])+"  "+theme.TextDim().Render(r[1]))
	}
	if hp, ok := a.Active().(helpProvider); ok {
		secs := hp.HelpSections()
		if len(secs) > 0 {
			lines = append(lines, "",
				theme.TabActive().Render(a.Active().Meta().Title+" — here"), "")
			for _, r := range secs {
				lines = append(lines, "  "+keycap(r[0])+"  "+theme.TextDim().Render(r[1]))
			}
		}
	}
	return theme.HelpOverlay().Render(strings.Join(lines, "\n"))
}

// keycap renders one key fragment as a raised pill (F-026 P7).
func keycap(k string) string {
	return theme.Keycap().Render(padKey(k))
}

func padKey(k string) string { return fmt.Sprintf("%-16s", k) }
