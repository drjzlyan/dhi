package wizard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
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
	"We'll draft a brief of your project, check your git identity, learn",
	"how you write and how your team names its work, and hire your team.",
	"Everything starts on a default read from your repo: press enter to",
	"keep it, skip any step, and change it later in Settings (ctrl+p).",
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
	env *Env
	// members are discovered once per directory, so going back and
	// forth keeps what was picked.
	members []setup.MemberSpec
	picked  []bool
	from    string
	cur     int
	scroll  kit.Scroller
	err     string
}

// pickerRows caps the repo list so a directory full of repos still fits
// the card; the window follows the cursor.
const pickerRows = 10

func (*workspaceStep) ID() string    { return "workspace" }
func (*workspaceStep) Title() string { return "workspace" }

// Applies only while no workspace exists; once created it drops out.
func (s *workspaceStep) Applies() bool { return s.env.Root == "" }

func (s *workspaceStep) Enter() tea.Cmd {
	s.err = ""
	if s.members != nil && s.from == s.env.CWD {
		return nil
	}
	s.members, s.picked, s.cur, s.from = nil, nil, 0, s.env.CWD
	if s.env.Discover != nil {
		s.members = s.env.Discover(s.env.CWD)
	}
	s.picked = make([]bool, len(s.members))
	s.scroll = kit.NewScroller(len(s.members), pickerRows, 0)
	if !s.choosing() {
		for i := range s.picked { // nothing to choose: the one candidate
			s.picked[i] = true
		}
	}
	return nil
}

// choosing reports several repos were found: the user picks which join.
// Enter with nothing picked takes the default (every repo found); "later"
// creates nothing, so no default is used behind the user's back.
func (s *workspaceStep) choosing() bool { return len(s.members) > 1 }

// Keys are the picker's own keys for the status row.
func (s *workspaceStep) Keys() []string {
	if !s.choosing() {
		return nil
	}
	return []string{"space select", "a all", "l later"}
}

func (s *workspaceStep) HandleKey(key string) Action {
	if !s.choosing() {
		return s.base.HandleKey(key)
	}
	switch key {
	case "j", "down":
		if s.cur < len(s.members)-1 {
			s.cur++
		}
		s.scroll.EnsureVisible(s.cur)
	case "k", "up":
		if s.cur > 0 {
			s.cur--
		}
		s.scroll.EnsureVisible(s.cur)
	case "space", " ", "x":
		s.picked[s.cur] = !s.picked[s.cur]
		s.err = ""
	case "a":
		all := s.count() < len(s.members)
		for i := range s.picked {
			s.picked[i] = all
		}
		s.err = ""
	case "enter":
		return Next // nothing picked: Apply takes the default, every repo
	case "l":
		return Skip // the user will choose later: create nothing now
	case "esc":
		return Skip
	}
	return Stay
}

func (s *workspaceStep) count() int {
	n := 0
	for _, p := range s.picked {
		if p {
			n++
		}
	}
	return n
}

func (s *workspaceStep) selected() []setup.MemberSpec {
	var out []setup.MemberSpec
	for i, m := range s.members {
		if s.picked[i] {
			out = append(out, m)
		}
	}
	return out
}

func (s *workspaceStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("No DHI workspace here yet."),
		dim(s.env.CWD),
		"",
	}
	if s.choosing() {
		out = append(out, theme.TextStyle().Render(fmt.Sprintf(
			"Found %d repos. Which should join the workspace?", len(s.members))))
		start, end := s.scroll.Window()
		if start > 0 {
			out = append(out, dim(fmt.Sprintf("    ↑ %d more", start)))
		}
		for i := start; i < end; i++ {
			m := s.members[i]
			mark := dim(theme.GlyphTodo)
			if s.picked[i] {
				mark = theme.SuccessText().Render(theme.GlyphCheck)
			}
			cursor := " "
			name := theme.TextStyle().Render(m.Name)
			if i == s.cur {
				cursor = theme.AccentText().Render(theme.GlyphCursor)
				name = theme.AccentText().Render(m.Name)
			}
			out = append(out, cursor+" "+mark+" "+name+dim("  →  "+m.Path))
		}
		if end < len(s.members) {
			out = append(out, dim(fmt.Sprintf("    ↓ %d more", len(s.members)-end)))
		}
		out = append(out, "")
		note := fmt.Sprintf("%d of %d selected · enter adds your selection.", s.count(), len(s.members))
		if s.count() == 0 {
			note = fmt.Sprintf("None selected · enter adds all %d.", len(s.members))
		}
		note += " Press l to choose later: nothing is created until you do (ctrl+p → Run setup wizard)."
		for _, l := range kit.WrapWords(note, w) {
			out = append(out, dim(l))
		}
	} else {
		out = append(out, theme.TextStyle().Render("Create one with these members:"))
		for _, m := range s.members {
			out = append(out, "  "+theme.AccentText().Render(m.Name)+dim("  →  "+m.Path))
		}
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
	chosen := s.selected()
	if len(chosen) == 0 {
		chosen = s.members // no choice made: the default is every repo found
	}
	members, err := s.env.InitWorkspace(s.env.CWD, chosen)
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
	id   gitcore.Identity
	err  error
	sugs []gitcore.Suggestion // when unset: where a name/email might come from
}

type identityStep struct {
	base
	env   *Env
	phase identityPhase
	found gitcore.Identity
	sugs  []gitcore.Suggestion
	best  gitcore.Suggestion
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
	resolve, suggest := s.env.Identity, s.env.SuggestIdentity
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		id, err := resolve(ctx)
		var sugs []gitcore.Suggestion
		if errors.Is(err, gitcore.ErrIdentityUnset) && suggest != nil {
			sugs = suggest(ctx)
		}
		return identityMsg{id, err, sugs}
	}
}

func (s *identityStep) Update(msg tea.Msg) tea.Cmd {
	m, isMsg := msg.(identityMsg)
	if !isMsg {
		return nil
	}
	if m.err == nil {
		s.found, s.phase = m.id, idFound
		s.env.WhoName = m.id.Name
		return nil
	}
	if !errors.Is(m.err, gitcore.ErrIdentityUnset) {
		// Git itself failed (not installed, unreadable config): collecting
		// a name would only fail again at write time.
		s.phase, s.err = idUnavailable, m.err.Error()
		return nil
	}
	s.sugs = m.sugs
	s.best = gitcore.BestSuggestion(m.sugs)
	s.collect(s.best.Identity)
	return nil
}

// collect opens the name/email form prefilled with id.
func (s *identityStep) collect(id gitcore.Identity) {
	s.phase = idCollect
	s.form = kit.NewForm("git identity",
		kit.NewTextField("name", id.Name), kit.NewTextField("email", id.Email))
}

func (s *identityStep) HandleKey(key string) Action {
	switch s.phase {
	case idChecking:
		if key == "esc" { // a slow git must not hold the wizard hostage
			return Skip
		}
		return Stay
	case idFound:
		if key == "e" {
			s.best = gitcore.Suggestion{}
			s.collect(s.found)
			return Stay
		}
		return s.base.HandleKey(key)
	case idUnavailable:
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
	s.env.WhoName = s.want.Name
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
			dim("enter uses it · e changes it"),
		}
	case idCollect:
		head := "Commits need an author. Who are you?"
		if s.found.Name != "" {
			head = "Change the author on your commits."
		}
		out = append([]string{
			theme.TextStyle().Render(head),
			dim("Saved to your global git config — nothing else changes."), "",
		}, formLines(s.form)...)
		if s.best.Source != "" {
			out = append(out, "", ok("suggested — "+s.best.Source))
			for _, sg := range s.sugs {
				who := strings.TrimSpace(sg.Name + " <" + sg.Email + ">")
				out = append(out, dim("  "+strings.ReplaceAll(who, " <>", "")+" · "+sg.Source))
			}
		}
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
	// start is the config the form edits: the saved conventions, plus
	// what was learned from the repo when nothing was customised (F-082).
	start    conventions.Config
	evidence []string
	tplIdx   int
}

// fieldTemplate is the naming-template toggle, last so the other
// fields keep their places.
const fieldTemplate = 5

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
	s.start, s.evidence, s.tplIdx = s.env.Conventions, nil, 0
	if s.env.LearnConventions != nil && s.start.Active == "" &&
		reflect.DeepEqual(s.start.Commit, conventions.Defaults().Commit) {
		if inf := s.env.LearnConventions(); !inf.Empty() {
			s.start.Learn(inf)
			s.evidence = inf.Evidence
		}
	}
	s.build(s.start, 0, nil)
	return nil
}

// templateChoices is the toggle: the starting rules, then the built-ins.
func (s *conventionsStep) templateChoices() []string {
	first := "current"
	if len(s.evidence) > 0 {
		first = "detected"
	}
	out := []string{first}
	for _, b := range conventions.Builtins() {
		out = append(out, b.Name)
	}
	return out
}

// build (re)creates the form from c, keeping the text fields' values
// (keep, when set) and the cursor.
func (s *conventionsStep) build(c conventions.Config, cur int, keep []string) {
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
	license := c.Copyright.License
	if keep != nil {
		co, holder, license = keep[2], keep[3], keep[4]
	}
	s.form = kit.NewForm("conventions",
		kit.NewToggleField("commits", commitFormats, indexOf(commitFormats, c.Commit.Format)),
		kit.NewToggleField("branch", branches, bi),
		kit.NewTextField("co-author", co),
		kit.NewTextField("copyright", holder),
		kit.NewTextField("license", license),
		kit.NewToggleField("template", s.templateChoices(), s.tplIdx),
	)
	for s.form.Cur() != cur {
		s.form.HandleKey("tab")
	}
}

// templated is the start config with the chosen template applied.
func (s *conventionsStep) templated() conventions.Config {
	c := s.start
	if s.tplIdx > 0 {
		_ = c.Activate(s.templateChoices()[s.tplIdx])
	}
	return c
}

func (s *conventionsStep) HandleKey(key string) Action {
	switch s.form.HandleKey(key) {
	case kit.FormCancel:
		return Skip
	case kit.FormSubmit:
		return Next
	}
	// Picking a template shows its commit format and branch above.
	if i := s.form.ToggleIndex(fieldTemplate); i != s.tplIdx {
		s.tplIdx = i
		s.build(s.templated(), fieldTemplate, s.form.Values())
	}
	return Stay
}

func (s *conventionsStep) CapturesText() bool { return true }

// next derives the Config the form describes from the current one.
func (s *conventionsStep) next() conventions.Config {
	v := s.form.Values()
	c := s.templated()
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
	if reflect.DeepEqual(next, s.env.Conventions) {
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
	who := "just for you"
	if s.env.Root != "" {
		who = "shared with your team"
	}
	s.env.Applied = append(s.env.Applied, "conventions saved — "+who)
	return nil
}

func (s *conventionsStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("How should your team's work look?"),
		dim("←/→ cycles · leave co-author and copyright empty to keep them off."), "",
	}
	out = append(out, formLines(s.form)...)
	if len(s.evidence) > 0 {
		out = append(out, "", ok("learned from your repo"))
		for _, e := range s.evidence {
			out = append(out, dim("  "+e))
		}
	}
	out = append(out, "",
		dim("template: a ready-made naming scheme (branches, commit subjects, PR text)."),
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
	if len(s.env.Skipped) > 0 {
		out = append(out, dim("skipped: "+strings.Join(s.env.Skipped, ", ")+" — re-run setup to pick them up"))
		if s.env.Root == "" {
			out = append(out, dim("no workspace yet: the brief, team and integrations wait for one"))
		}
	}
	out = append(out, "")
	verb := "start"
	if s.env.Changed {
		verb = "restart DHI and start"
	}
	out = append(out, theme.TextStyle().Render("Press enter to "+verb+"."))
	if s.env.StartTour != nil || s.env.QueueTour != nil {
		out = append(out, theme.AccentText().Render("Press t")+theme.TextStyle().Render(" for a 2-minute guided tour first."))
	}
	out = append(out, dim("Re-run any time: ctrl+p → Run setup wizard · lessons: ctrl+p → Tutorial."))
	return out
}

// HandleKey: enter finishes; t finishes and asks for the tour.
func (s *doneStep) HandleKey(key string) Action {
	if key == "t" && (s.env.StartTour != nil || s.env.QueueTour != nil) {
		s.env.TourRequested = true
		return Next
	}
	return s.base.HandleKey(key)
}
