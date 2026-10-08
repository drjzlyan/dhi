package wizard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// base supplies the no-op defaults most steps share.
type base struct{}

func (base) Applies() bool          { return true }
func (base) Enter() tea.Cmd         { return nil }
func (base) Update(tea.Msg) tea.Cmd { return nil }
func (base) Apply() error           { return nil }
func (base) HandleKey(k string) Action {
	switch k {
	case "enter":
		return Next
	case "esc":
		return Skip
	}
	return Stay
}

func dim(s string) string  { return theme.TextDim().Render(s) }
func ok(s string) string   { return theme.SuccessText().Render(theme.GlyphCheck + " " + s) }
func warn(s string) string { return theme.WarningText().Render(s) }
func bad(s string) string  { return theme.DangerText().Render(theme.GlyphCross + " " + s) }

// formLines renders a kit.Form without its own hint (the wizard has one).
func formLines(f *kit.Form) []string {
	l := f.View()
	return l[:len(l)-1]
}

// ---- welcome ----

type welcomeStep struct {
	base
	env     *Env
	version string
}

func (*welcomeStep) ID() string    { return "welcome" }
func (*welcomeStep) Title() string { return "welcome" }

var welcomeLines = []string{
	"Welcome. Let's get DHI ready — about a minute.",
	"",
	"We'll check your workspace, your git identity and your team's",
	"conventions. Everything has a sensible default: skip any step and",
	"change it later from Settings or the command palette (ctrl+p).",
}

func (s *welcomeStep) View(w, f int) []string {
	out := strings.Split(branding.HeroBlock(s.version), "\n")
	out = append(out, "")
	// The reveal is a pure function of the frame count: 10 runes per tick
	// (~3s for the whole intro — long enough to feel alive, short enough
	// that nobody waits on it).
	text := kit.Typewriter(strings.Join(welcomeLines, "\n"), f*10)
	for _, l := range strings.Split(text, "\n") {
		out = append(out, theme.TextStyle().Render(l))
	}
	return out
}

// ---- workspace ----

type workspaceStep struct {
	base
	env     *Env
	members []setup.MemberSpec
	err     string
}

func (*workspaceStep) ID() string    { return "workspace" }
func (*workspaceStep) Title() string { return "workspace" }

// Applies only while no workspace exists; once created it drops out.
func (s *workspaceStep) Applies() bool { return s.env.Root == "" }

func (s *workspaceStep) Enter() tea.Cmd {
	s.err = ""
	s.members = nil
	if s.env.Discover != nil {
		s.members = s.env.Discover(s.env.CWD)
	}
	return nil
}

func (s *workspaceStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("No DHI workspace here yet."),
		dim(s.env.CWD),
		"",
		theme.TextStyle().Render("Create one with these members:"),
	}
	for _, m := range s.members {
		out = append(out, "  "+theme.AccentText().Render(m.Name)+dim("  →  "+m.Path))
	}
	out = append(out, "",
		dim("Writes .dhi/workspace.toml and a .dhi/.gitignore that keeps chat,"),
		dim("memory and task state out of version control."))
	if s.err != "" {
		out = append(out, "", bad(s.err))
	}
	return out
}

func (s *workspaceStep) Apply() error {
	if s.env.InitWorkspace == nil {
		s.err = "workspace creation is unavailable in this build"
		return fmt.Errorf("%s", s.err)
	}
	members, err := s.env.InitWorkspace(s.env.CWD, s.members)
	if err != nil {
		s.err = err.Error()
		return err
	}
	s.env.Root = s.env.CWD
	s.env.Changed = true
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}
	s.env.Applied = append(s.env.Applied,
		"created workspace "+filepath.Base(s.env.CWD)+" ("+strings.Join(names, ", ")+")")
	return nil
}

// ---- identity ----

type identityPhase uint8

const (
	idChecking identityPhase = iota
	idFound
	idCollect
	idConfirm
	idUnavailable
)

type identityMsg struct {
	id  gitcore.Identity
	err error
}

type identityStep struct {
	base
	env   *Env
	phase identityPhase
	found gitcore.Identity
	form  *kit.Form
	want  gitcore.Identity
	err   string
}

func (*identityStep) ID() string    { return "identity" }
func (*identityStep) Title() string { return "identity" }

func (s *identityStep) Enter() tea.Cmd {
	s.err = ""
	if s.env.Identity == nil {
		s.phase = idUnavailable
		return nil
	}
	s.phase = idChecking
	resolve := s.env.Identity
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		id, err := resolve(ctx)
		return identityMsg{id, err}
	}
}

func (s *identityStep) Update(msg tea.Msg) tea.Cmd {
	m, isMsg := msg.(identityMsg)
	if !isMsg {
		return nil
	}
	if m.err == nil {
		s.found, s.phase = m.id, idFound
		return nil
	}
	if !errors.Is(m.err, gitcore.ErrIdentityUnset) {
		// Git itself failed (not installed, unreadable config): collecting
		// a name would only fail again at write time.
		s.phase, s.err = idUnavailable, m.err.Error()
		return nil
	}
	s.phase = idCollect
	s.form = kit.NewForm("git identity",
		kit.NewTextField("name", ""), kit.NewTextField("email", ""))
	return nil
}

func (s *identityStep) HandleKey(key string) Action {
	switch s.phase {
	case idChecking:
		return Stay
	case idFound, idUnavailable:
		return s.base.HandleKey(key)
	case idCollect:
		switch s.form.HandleKey(key) {
		case kit.FormCancel:
			return Skip
		case kit.FormSubmit:
			v := s.form.Values()
			s.want = gitcore.Identity{Name: strings.TrimSpace(v[0]), Email: strings.TrimSpace(v[1])}
			if err := gitcore.ValidateIdentity(s.want); err != nil {
				s.form.SetError(err.Error())
				return Stay
			}
			s.phase = idConfirm
		}
		return Stay
	case idConfirm:
		switch key {
		case "enter", "y":
			return Next
		case "esc", "n":
			s.phase = idCollect
		}
		return Stay
	}
	return Stay
}

// CapturesText reports free-text entry (the gate owns the keyboard, but
// tests and the shell can ask).
func (s *identityStep) CapturesText() bool { return s.phase == idCollect }

func (s *identityStep) Apply() error {
	// Only the confirm phase writes; found/unavailable have nothing to apply.
	if s.phase != idConfirm {
		return nil
	}
	if s.env.SetIdentity == nil {
		s.err = "cannot write git config: hermetic git is not installed"
		s.phase = idUnavailable
		return fmt.Errorf("%s", s.err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.env.SetIdentity(ctx, s.want); err != nil {
		s.err = err.Error()
		return err
	}
	s.env.Applied = append(s.env.Applied, "git identity set to "+s.want.Name+" <"+s.want.Email+">")
	return nil
}

func (s *identityStep) View(w, f int) []string {
	var out []string
	switch s.phase {
	case idChecking:
		return []string{kit.SpinnerGlyph(f) + " " + dim("reading your git identity…")}
	case idFound:
		out = []string{
			ok("git identity found"),
			"  " + theme.TextStyle().Render(s.found.Name) + dim(" <"+s.found.Email+">"),
			"",
			dim("Commits made by you and your agents carry this author."),
		}
	case idCollect:
		out = append([]string{
			theme.TextStyle().Render("Commits need an author. Who are you?"),
			dim("Saved to your global git config — nothing else changes."), "",
		}, formLines(s.form)...)
	case idConfirm:
		out = []string{theme.TextStyle().Render("DHI will run:"), ""}
		for _, c := range gitcore.IdentityCommands(s.want) {
			out = append(out, "  "+theme.AccentText().Render(c))
		}
		out = append(out, "", dim("enter / y to run · esc / n to edit"))
	case idUnavailable:
		out = []string{
			warn("Git isn't installed in DHI's toolchain yet."),
			dim("Run these yourself when it is, or re-run setup from the palette:"), "",
		}
		for _, c := range gitcore.IdentityCommands(gitcore.Identity{Name: "Your Name", Email: "you@example.com"}) {
			out = append(out, "  "+theme.AccentText().Render(c))
		}
	}
	if s.err != "" {
		out = append(out, "", bad(s.err))
	}
	return out
}

// ---- conventions ----

var (
	commitFormats = []string{conventions.FormatFree, conventions.FormatConventional, conventions.FormatTicket}
	branchPresets = []string{"task/{slug}", "feature/{slug}", "{user}/{slug}", "{user}/{date}-{slug}"}
)

type conventionsStep struct {
	base
	env  *Env
	form *kit.Form
}

func (*conventionsStep) ID() string    { return "conventions" }
func (*conventionsStep) Title() string { return "conventions" }

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}

func (s *conventionsStep) Enter() tea.Cmd {
	c := s.env.Conventions
	branches := branchPresets
	bi := indexOf(branches, c.Branch.Task)
	if bi < 0 { // a hand-edited pattern stays selectable
		branches = append([]string{c.Branch.Task}, branches...)
		bi = 0
	}
	co := ""
	if c.Commit.CoAuthorEnabled {
		co = c.Commit.CoAuthor
	}
	holder := ""
	if c.Copyright.Enabled {
		holder = c.Copyright.Holder
	}
	s.form = kit.NewForm("conventions",
		kit.NewToggleField("commits", commitFormats, indexOf(commitFormats, c.Commit.Format)),
		kit.NewToggleField("branch", branches, bi),
		kit.NewTextField("co-author", co),
		kit.NewTextField("copyright", holder),
		kit.NewTextField("license", c.Copyright.License),
	)
	return nil
}

func (s *conventionsStep) HandleKey(key string) Action {
	switch s.form.HandleKey(key) {
	case kit.FormCancel:
		return Skip
	case kit.FormSubmit:
		return Next
	}
	return Stay
}

func (s *conventionsStep) CapturesText() bool { return true }

// next derives the Config the form describes from the current one.
func (s *conventionsStep) next() conventions.Config {
	v := s.form.Values()
	c := s.env.Conventions
	c.Commit.Format = v[0]
	c.Branch.Task = v[1]
	c.Commit.CoAuthor = strings.TrimSpace(v[2])
	c.Commit.CoAuthorEnabled = c.Commit.CoAuthor != ""
	c.Copyright.Holder = strings.TrimSpace(v[3])
	c.Copyright.Enabled = c.Copyright.Holder != ""
	c.Copyright.License = strings.TrimSpace(v[4])
	return c
}

func (s *conventionsStep) Apply() error {
	next := s.next()
	if err := next.Validate(); err != nil {
		s.form.SetError(err.Error())
		return err
	}
	if next == s.env.Conventions {
		return nil
	}
	if s.env.SaveConventions == nil {
		err := fmt.Errorf("conventions cannot be saved in this build")
		s.form.SetError(err.Error())
		return err
	}
	if err := s.env.SaveConventions(s.env.Root, next); err != nil {
		s.form.SetError(err.Error())
		return err
	}
	s.env.Conventions = next
	s.env.Changed = true
	where := "your user config"
	if s.env.Root != "" {
		where = ".dhi/conventions.toml (shared with your team)"
	}
	s.env.Applied = append(s.env.Applied, "conventions saved to "+where)
	return nil
}

func (s *conventionsStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("How should your team's work look?"),
		dim("←/→ cycles · leave co-author and copyright empty to keep them off."), "",
	}
	out = append(out, formLines(s.form)...)
	out = append(out, "",
		dim("co-author: \"Name <email>\", added to commits as a Co-Authored-By trailer."),
		dim("copyright: the holder named in new-file headers; license is an SPDX id (MIT)."))
	return out
}

// ---- done ----

type doneStep struct {
	base
	env *Env
}

func (*doneStep) ID() string    { return "done" }
func (*doneStep) Title() string { return "done" }

func (s *doneStep) View(w, f int) []string {
	out := []string{theme.SuccessText().Render(theme.GlyphSpark + " You're set up."), ""}
	if len(s.env.Applied) == 0 {
		out = append(out, dim("Nothing needed changing."))
	}
	for _, a := range s.env.Applied {
		out = append(out, ok(a))
	}
	out = append(out, "")
	if s.env.Changed {
		out = append(out, theme.TextStyle().Render("DHI will restart to apply these — press enter."))
	} else {
		out = append(out, theme.TextStyle().Render("Press enter to start."))
	}
	out = append(out, dim("Re-run any time: ctrl+p → Run setup wizard."))
	return out
}
