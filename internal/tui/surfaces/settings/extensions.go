package settings

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/assign"
	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// SKILLS and INTEGRATIONS (ADR-0043, F-105/F-107): what agents can use,
// in the formats every coding agent reads — Agent Skills, the mcpServers
// JSON, and vendor plugins from marketplaces. Every install previews what
// will land, and ends with choosing which bots get it.

// Extensions wires installs. Zero scopes disable that destination.
type Extensions struct {
	Fetch        install.Fetcher
	HTTP         *http.Client
	Tmp, Cache   string        // a temp dir for checkouts; the marketplace cache
	Workspace    install.Scope // .dhi/skills, .dhi/mcp.json, .dhi/installed.toml
	User         install.Scope // <config>/dhi/...
	Marketplaces []string      // catalog sources; empty = install.DefaultMarketplace
	// Status names a server's sign-in state ("signed in", "sign in on
	// first use", "needs a token") — F-106; nil shows none.
	Status func(srv mcpserver.Server) string
	// Login signs in to an http server now (F-106); nil hides enter.
	Login func(ctx context.Context, srv mcpserver.Server) error
	// Logout forgets a server's tokens (F-106); nil hides d.
	Logout func(srv mcpserver.Server) error
	// Signing reports a sign-in waiting in the browser; PasteSignIn
	// finishes it with the address the browser ended on (SSH).
	Signing     func(srv mcpserver.Server) bool
	PasteSignIn func(srv mcpserver.Server, addr string) error
	// SignInNow opens the sign-in for newly added servers that need one.
	SignInNow func(servers []mcpserver.Server)
}

const (
	scopeHere       = "this workspace"
	scopeEverywhere = "every workspace"
)

func (e *Extensions) scope(name string) install.Scope {
	if name == scopeEverywhere {
		return e.User
	}
	return e.Workspace
}

func (e *Extensions) scopeNames() []string {
	var out []string
	if e.Workspace.Name != "" {
		out = append(out, scopeHere)
	}
	if e.User.Name != "" {
		out = append(out, scopeEverywhere)
	}
	return out
}

func (e *Extensions) markets() []string {
	if len(e.Marketplaces) > 0 {
		return e.Marketplaces
	}
	return []string{install.DefaultMarketplace}
}

// extState is the install flow and the open pickers.
type extState struct {
	skillCur, intCur int
	bundle           *install.Bundle // fetched, waiting for confirmation
	scope            install.Scope
	plugins          []install.PluginEntry // the marketplaces' plugins, once loaded
	picker           *kit.Palette          // plugin / add-server picker
	assign           *assignState
	queue            []assign.Item // more items to assign after this one
}

// extEvent carries an async fetch result to the loop.
type extEvent struct {
	bundle  *install.Bundle
	scope   install.Scope
	plugins []install.PluginEntry
	err     string
	note    string
}

func (m *Model) ext() *Extensions { return m.d.Ext }

// ---- SKILLS ----

type skillRow struct {
	k      *library.Skill
	source string
	users  []string
	all    bool // given to every bot
}

func (m *Model) skillRows() []skillRow {
	lib := m.libStore()
	if lib == nil {
		lib = library.Open(nil) // no workspace: built-ins and yours still list
	}
	roster, _ := m.roster()
	pol := m.allBotsPolicy()
	var out []skillRow
	for _, e := range lib.Skills() {
		k, _ := lib.Skill(e.Slug)
		r := skillRow{k: k, source: e.Source,
			users: assign.Holders(roster, assign.Item{Kind: assign.KindSkill, Name: e.Slug})}
		for _, s := range pol.Skills {
			r.all = r.all || s == e.Slug
		}
		out = append(out, r)
	}
	return out
}

func sourceLabel(src string) string {
	switch src {
	case library.SourceBuiltin:
		return "built in"
	case library.SourceYours:
		return "every workspace"
	case library.SourceLocal:
		return "this workspace"
	case library.SourceAgents:
		return "~/.agents"
	case library.SourceMember:
		return "repo"
	}
	return src
}

func (m *Model) skillsView(w int) []string {
	out := []string{theme.TextDim().Render("Agent Skills (SKILL.md) your bots can use · i install from GitHub, a git URL or a folder"), ""}
	rows := m.skillRows()
	if len(rows) == 0 {
		return append(out, theme.TextDim().Render("(no skills — press i to install one)"))
	}
	st := &m.extState
	st.skillCur = min(st.skillCur, len(rows)-1)
	from, to := window(len(rows), st.skillCur, m.listRows(4))
	if from > 0 {
		out = append(out, theme.TextMuted().Render(fmt.Sprintf("  ↑ %d more", from)))
	}
	for i := from; i < to; i++ {
		r := rows[i]
		label := padTo(kit.ClipEllipsis(r.k.Slug, 23), 24)
		mark, name := "  ", theme.TextStyle().Render(label)
		if i == st.skillCur {
			mark = theme.AccentText().Render(theme.GlyphCursor) + " "
			name = theme.AccentText().Render(label)
		}
		who := theme.TextMuted().Render("no bots")
		switch {
		case r.all:
			who = kit.Badge(theme.BadgeOK, "all bots")
		case len(r.users) > 0:
			who = theme.TextDim().Render(kit.ClipEllipsis(strings.Join(r.users, ", "), 22))
		}
		line := mark + name + theme.TextMuted().Render(padTo(sourceLabel(r.source), 17)) + who
		out = append(out, kit.ClipEllipsis(line, w))
		if i == st.skillCur {
			out = append(out, "    "+theme.Hint().Render(kit.ClipEllipsis(r.k.Description, w-4)))
		}
	}
	if to < len(rows) {
		out = append(out, theme.TextMuted().Render(fmt.Sprintf("  ↓ %d more", len(rows)-to)))
	}
	if lib := m.libStore(); lib != nil {
		for _, warn := range lib.Warnings() {
			if strings.HasPrefix(warn, "skill") {
				out = append(out, theme.WarningText().Render("⚠ "+kit.ClipEllipsis(warn, w-2)))
			}
		}
	}
	return out
}

func (m *Model) skillsKey(key string) bool {
	rows := m.skillRows()
	st := &m.extState
	var sel *skillRow
	if st.skillCur < len(rows) {
		sel = &rows[st.skillCur]
	}
	switch key {
	case "j", "down":
		if st.skillCur < len(rows)-1 {
			st.skillCur++
		}
	case "k", "up":
		if st.skillCur > 0 {
			st.skillCur--
		}
	case "i":
		m.openInstallSource()
	case "g":
		if sel != nil {
			m.openAssign(assign.Item{Kind: assign.KindSkill, Name: sel.k.Slug, Description: sel.k.Description}, nil)
		}
	case "a":
		if sel != nil {
			m.openAgentWith("skills", sel.k.Slug)
		}
	case "w":
		m.openSkillAuthor()
	case "s":
		if sel != nil {
			m.allowScripts(sel)
		}
	case "x":
		if sel != nil {
			if sel.source != library.SourceLocal && sel.source != library.SourceYours {
				m.flash = "failed: " + sel.k.Slug + " is " + sourceLabel(sel.source) + "; remove it where it was installed"
				return true
			}
			line := "remove skill " + sel.k.Slug + "?"
			if len(sel.users) > 0 {
				line += " " + strings.Join(sel.users, ", ") + " lose it."
			}
			m.openConfirmDialog("remove skill", line, sel.k.Slug, dlgSkillRemove)
		}
	case "enter", "v":
		if sel != nil {
			m.openDisplayDialog("skill — "+sel.k.Slug, skillCard(sel.k))
		}
	default:
		return false
	}
	return true
}

func skillCard(k *library.Skill) []string {
	lines := []string{theme.TextDim().Render(k.Description), ""}
	if loc := k.Location(); loc != "" {
		lines = append(lines, theme.Hint().Render(loc), "")
	}
	lines = append(lines, strings.Split(k.Body, "\n")...)
	if files, more := install.ListFiles(k.Dir); len(files) > 0 {
		lines = append(lines, "", theme.SectionHeader().Render("FILES"))
		for _, f := range files {
			lines = append(lines, "  "+f)
		}
		if more {
			lines = append(lines, "  …")
		}
	}
	return lines
}

func (m *Model) allowScripts(r *skillRow) {
	e := m.ext()
	if e == nil {
		m.flash = "failed: installs are unavailable here"
		return
	}
	sc := e.Workspace
	if r.source == library.SourceYours {
		sc = e.User
	}
	if r.source != library.SourceLocal && r.source != library.SourceYours {
		m.flash = "failed: only skills installed here can have their scripts allowed"
		return
	}
	files := install.Runnable(r.k.Dir)
	if len(files) == 0 {
		m.flash = r.k.Slug + " has no scripts"
		return
	}
	m.openConfirmDialog("allow scripts", "let bots run "+strings.Join(files, ", ")+" from "+r.k.Slug+"? (sandboxed; v shows the skill)", sc.Name+"|"+r.k.Slug, dlgSkillScripts)
}

func (m *Model) openSkillAuthor() {
	e := m.ext()
	if e == nil || len(e.scopeNames()) == 0 {
		m.flash = "failed: no place to save skills here"
		return
	}
	m.openFormDialog("write a skill (SKILL.md)", dlgSkillAuthor, "",
		kit.NewTextField("name        ", ""),
		kit.NewTextField("description ", ""),
		kit.NewTextField("instructions", ""),
		kit.NewToggleField("save for    ", e.scopeNames(), 0))
	m.dlgExtra = []string{theme.Hint().Render("name: lowercase-with-hyphens · description: what it does and when to use it")}
}

func (m *Model) submitSkillAuthor() {
	v := m.dform.Values()
	sc := m.ext().scope(v[3])
	k := &library.Skill{Name: strings.TrimSpace(v[0]), Description: strings.TrimSpace(v[1]), Body: v[2]}
	if err := library.WriteSkillAt(sc.SkillsDir, k); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.reloadLib()
	m.openAssign(assign.Item{Kind: assign.KindSkill, Name: k.Name, Description: k.Description}, nil)
}

// ---- install flow (SKILLS i, INTEGRATIONS plugins) ----

func (m *Model) openInstallSource() {
	e := m.ext()
	if e == nil || len(e.scopeNames()) == 0 {
		m.flash = "failed: installs are unavailable here"
		return
	}
	m.openFormDialog("install skills", dlgExtSource, "",
		kit.NewTextField("from       ", ""),
		kit.NewToggleField("install for", e.scopeNames(), 0))
	m.dlgExtra = []string{theme.Hint().Render("a GitHub URL (…/tree/main/skills/pdf), owner/repo, a git URL, or a folder")}
}

func (m *Model) submitInstallSource() {
	v := m.dform.Values()
	src, err := install.ParseSource(v[0])
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.dform.Busy = true
	m.startPreview(src, m.ext().scope(v[1]))
}

// startPreview fetches src off the UI loop; the confirmation opens when
// the result arrives.
func (m *Model) startPreview(src install.Source, sc install.Scope) {
	e := m.ext()
	m.flash = "fetching " + src.String() + "…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		b, err := install.Preview(ctx, src, e.Fetch, e.Tmp)
		ev := extEvent{bundle: b, scope: sc}
		if err != nil {
			ev.err = err.Error()
		}
		m.events <- settingsEvent{ext: &ev}
	}()
}

func (m *Model) onExtEvent(ev extEvent) {
	st := &m.extState
	if ev.plugins != nil || (ev.err != "" && ev.bundle == nil && m.dkind != dlgExtSource) {
		if ev.err != "" {
			m.flash = "failed: " + ev.err
			return
		}
		st.plugins = ev.plugins
		m.flash = ev.note
		m.openAddPicker()
		return
	}
	if m.dlg != nil && m.dkind == dlgExtSource {
		m.closeDialog()
	}
	if ev.err != "" {
		m.flash = "failed: " + ev.err
		return
	}
	if ev.bundle.Marketplace != nil { // a marketplace: pick one of its plugins
		ev.bundle.Close()
		st.plugins = ev.bundle.Marketplace.Plugins
		m.openAddPicker()
		return
	}
	st.bundle, st.scope = ev.bundle, ev.scope
	m.openInstallConfirm()
}

func (m *Model) openInstallConfirm() {
	b := m.extState.bundle
	var lines []string
	if b.Plugin != nil {
		lines = append(lines, theme.TextStyle().Render("plugin "+b.Plugin.Name)+theme.TextDim().Render(" — "+b.Plugin.Description))
		if len(b.Plugin.Unused) > 0 {
			lines = append(lines, theme.TextMuted().Render("its "+strings.Join(b.Plugin.Unused, ", ")+" are not used by DHI"))
		}
	}
	lines = append(lines, theme.TextDim().Render("from "+b.Source.String()+pinned(b.Commit)), "")
	scripts := false
	for _, c := range b.Skills {
		lines = append(lines, theme.AccentText().Render("skill "+c.Skill.Slug)+theme.TextDim().Render(" — "+kit.ClipEllipsis(c.Skill.Description, 60)))
		for _, s := range c.Scripts {
			lines = append(lines, theme.WarningText().Render("   runs: "+s))
			scripts = true
		}
	}
	for _, s := range b.Servers {
		where := s.URL
		if s.Transport == mcpserver.Stdio {
			where = "runs " + s.Command + " " + strings.Join(s.Args, " ")
		}
		lines = append(lines, theme.AccentText().Render("server "+s.Slug)+theme.TextDim().Render(" — "+kit.ClipEllipsis(where, 60)))
	}
	for _, w := range b.Warnings {
		lines = append(lines, theme.WarningText().Render("⚠ "+w))
	}
	fields := []kit.Field{}
	if scripts {
		fields = append(fields, kit.NewToggleField("scripts", []string{"don't run yet", "allow them"}, 0))
	}
	title := fmt.Sprintf("install %s, %s for %s", kit.Count(len(b.Skills), "skill"), kit.Count(len(b.Servers), "server"), m.extState.scope.Name)
	if len(fields) == 0 {
		fields = append(fields, kit.NewToggleField("install", []string{"yes"}, 0))
	}
	m.openFormDialog(title, dlgExtConfirm, "", fields...)
	m.dlgExtra = append([]string{""}, lines...)
}

func pinned(commit string) string {
	if commit == "" {
		return ""
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return " @ " + commit
}

func (m *Model) submitInstallConfirm() {
	st := &m.extState
	b := st.bundle
	allow := m.dform.Values()[0] == "allow them"
	res, err := install.Apply(b, st.scope, nil, allow)
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	b.Close()
	st.bundle = nil
	m.closeDialog()
	m.reloadLib()
	var items []assign.Item
	for _, c := range b.Skills {
		for _, n := range res.Skills {
			if n == c.Skill.Slug {
				items = append(items, assign.Item{Kind: assign.KindSkill, Name: n, Description: c.Skill.Description})
			}
		}
	}
	desc := ""
	if b.Plugin != nil {
		desc = b.Plugin.Description
	}
	for _, n := range res.Servers {
		items = append(items, assign.Item{Kind: assign.KindServer, Name: n, Description: desc})
	}
	m.flash = fmt.Sprintf("installed %s", strings.Join(append(res.Skills, res.Servers...), ", "))
	m.signInInstalled(res.Servers)
	if len(items) > 0 {
		m.openAssign(items[0], items[1:])
	}
}

// ---- INTEGRATIONS ----

func (m *Model) servers() []mcpserver.Server {
	if m.d.WS == nil {
		return mcpserver.Open("").Servers()
	}
	return mcpserver.OpenWith(m.d.WS.Root, m.d.WS.Members()).Servers()
}

func (m *Model) integrationsView(w int) []string {
	out := []string{theme.TextDim().Render("MCP servers your bots can use · + add an official plugin, a URL or a pasted config"), ""}
	servers := m.servers()
	if len(servers) == 0 {
		return append(out, theme.TextDim().Render("(none yet — press + to add Atlassian, Slack, Zoom, Linear, Notion, GitHub and more)"))
	}
	st := &m.extState
	st.intCur = min(st.intCur, len(servers)-1)
	roster, _ := m.roster()
	pol := m.allBotsPolicy()
	from, to := window(len(servers), st.intCur, m.listRows(3))
	for i := from; i < to; i++ {
		s := servers[i]
		label := padTo(kit.ClipEllipsis(s.Slug, 19), 20)
		mark, name := "  ", theme.TextStyle().Render(label)
		if i == st.intCur {
			mark = theme.AccentText().Render(theme.GlyphCursor) + " "
			name = theme.AccentText().Render(label)
		}
		state := ""
		if e := m.ext(); e != nil && e.Status != nil {
			state = e.Status(s)
		}
		who := theme.TextMuted().Render("no bots")
		users := assign.Holders(roster, assign.Item{Kind: assign.KindServer, Name: s.Slug})
		all := false
		for _, n := range pol.Servers {
			all = all || n == s.Slug
		}
		switch {
		case all:
			who = kit.Badge(theme.BadgeOK, "all bots")
		case len(users) > 0:
			who = theme.TextDim().Render(kit.ClipEllipsis(strings.Join(users, ", "), 20))
		}
		where := s.Host()
		if s.Transport == mcpserver.Stdio {
			where = "local: " + s.Command
		}
		line := mark + name + theme.TextMuted().Render(padTo(kit.ClipEllipsis(where, 26), 28)) + padTo(state, 22) + who
		out = append(out, kit.ClipEllipsis(line, w))
	}
	return out
}

func (m *Model) integrationsKey(key string) bool {
	servers := m.servers()
	st := &m.extState
	var sel *mcpserver.Server
	if st.intCur < len(servers) {
		sel = &servers[st.intCur]
	}
	e := m.ext()
	switch key {
	case "j", "down":
		if st.intCur < len(servers)-1 {
			st.intCur++
		}
	case "k", "up":
		if st.intCur > 0 {
			st.intCur--
		}
	case "+", "i":
		m.loadPlugins(false)
	case "R":
		m.loadPlugins(true)
	case "g":
		if sel != nil {
			m.openAssign(assign.Item{Kind: assign.KindServer, Name: sel.Slug}, nil)
		}
	case "a":
		if sel != nil {
			m.openAgentWith("tools", assign.ServerTool(sel.Slug))
		}
	case "enter":
		if sel != nil && e != nil && e.Login != nil && sel.Transport == mcpserver.HTTP {
			srv := *sel
			m.flash = "opening the " + srv.Slug + " sign-in page in your browser…"
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				ev := settingsEvent{msg: "signed in to " + srv.Slug}
				if err := e.Login(ctx, srv); err != nil {
					ev = settingsEvent{err: err.Error()}
				}
				select {
				case m.events <- ev:
				default:
				}
			}()
		}
	case "p":
		if sel != nil && e != nil && e.Signing != nil && e.Signing(*sel) {
			m.openFormDialog("finish signing in to "+sel.Slug, dlgSignInPaste, sel.Slug,
				kit.NewTextField("address", ""))
			m.dlgExtra = []string{theme.Hint().Render("paste the address your browser ended on after you approved")}
		} else if sel != nil {
			m.flash = "no sign-in is waiting for " + sel.Slug
		}
	case "d":
		if sel != nil && e != nil && e.Logout != nil {
			if err := e.Logout(*sel); err != nil {
				m.flash = "failed: " + err.Error()
			} else {
				m.flash = "signed out of " + sel.Slug
			}
		}
	case "x":
		if sel != nil {
			if sel.Source == mcpserver.SourceRepo {
				m.flash = "failed: " + sel.Slug + " is in " + sel.File + "; remove it there"
				return true
			}
			m.openConfirmDialog("remove server", "remove "+sel.Slug+" from "+sel.File+"?", sel.Source+"|"+sel.Slug, dlgServerRemove)
		}
	case "v":
		if sel != nil {
			m.openDisplayDialog("server — "+sel.Slug, strings.Split(install.ServerJSON([]mcpserver.Server{*sel}), "\n"))
		}
	default:
		return false
	}
	return true
}

// loadPlugins fetches the marketplaces' catalogs, then opens the picker.
func (m *Model) loadPlugins(refresh bool) {
	e := m.ext()
	if e == nil || len(e.scopeNames()) == 0 {
		m.flash = "failed: installs are unavailable here"
		return
	}
	if len(m.extState.plugins) > 0 && !refresh {
		m.openAddPicker()
		return
	}
	m.flash = "loading the official plugins…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var all []install.PluginEntry
		var notes []string
		for _, raw := range e.markets() {
			src, err := install.ParseSource(raw)
			if err != nil {
				notes = append(notes, err.Error())
				continue
			}
			mk, warns, err := install.FetchMarketplace(ctx, src, e.HTTP, e.Fetch, e.Cache, refresh)
			if err != nil {
				notes = append(notes, err.Error())
				continue
			}
			_ = warns
			all = append(all, mk.Plugins...)
		}
		ev := extEvent{plugins: all, note: fmt.Sprintf("%s available", kit.Count(len(all), "plugin"))}
		if len(all) == 0 {
			ev.err = "no plugins: " + strings.Join(notes, "; ")
			ev.plugins = nil
		}
		m.events <- settingsEvent{ext: &ev}
	}()
}

// featured are the vendors' own plugins shown first in the picker.
var featured = []string{"atlassian", "slack", "zoom-plugin", "linear", "notion", "github", "gitlab", "asana", "figma", "sentry"}

func (m *Model) openAddPicker() {
	var items []kit.PaletteItem
	items = append(items,
		kit.PaletteItem{Group: "add", Title: "a server by URL", Hint: "https://…", Data: "url"},
		kit.PaletteItem{Group: "add", Title: "a pasted mcpServers snippet", Hint: "JSON", Data: "snippet"},
		kit.PaletteItem{Group: "add", Title: "skills from GitHub, a git URL or a folder", Hint: "SKILL.md", Data: "skills"})
	rank := map[string]int{}
	for i, n := range featured {
		rank[n] = i + 1
	}
	plugins := append([]install.PluginEntry(nil), m.extState.plugins...)
	sort.SliceStable(plugins, func(i, j int) bool {
		ri, rj := rank[plugins[i].Name], rank[plugins[j].Name]
		if (ri > 0) != (rj > 0) {
			return ri > 0
		}
		return ri < rj
	})
	for _, p := range plugins {
		p := p
		items = append(items, kit.PaletteItem{Group: orDash(p.Category), Title: p.Name + " — " + p.Description, Data: p})
	}
	pal := kit.NewPalette(items)
	pal.Title, pal.Noun = "add an integration", "choices"
	m.extState.picker = pal
}

func (m *Model) pickerKey(key string) {
	picked, closed := m.extState.picker.HandleKey(key)
	if !closed {
		return
	}
	m.extState.picker = nil
	if picked == nil {
		return
	}
	e := m.ext()
	switch d := picked.Data.(type) {
	case install.PluginEntry:
		m.openFormDialog("install plugin "+d.Name, dlgPluginScope, "",
			kit.NewToggleField("install for", e.scopeNames(), 0))
		m.dlgExtra = []string{"", theme.TextDim().Render(d.Description), theme.Hint().Render(d.Source.String())}
		m.pendingPlugin = &d
	case string:
		switch d {
		case "url":
			m.openFormDialog("add a server by URL", dlgServerURL, "",
				kit.NewTextField("name       ", ""),
				kit.NewTextField("url        ", "https://"),
				kit.NewToggleField("install for", e.scopeNames(), 0))
			m.dlgExtra = []string{theme.Hint().Render("you sign in on first use when the server needs it")}
		case "snippet":
			m.openFormDialog("paste a server config", dlgServerSnippet, "",
				kit.NewTextField("json       ", ""),
				kit.NewToggleField("install for", e.scopeNames(), 0))
			m.dlgExtra = []string{theme.Hint().Render(`as vendors publish it: {"mcpServers": {"name": {...}}}`)}
		case "skills":
			m.openInstallSource()
		}
	}
}

func (m *Model) submitPluginScope() {
	p := m.pendingPlugin
	m.pendingPlugin = nil
	if p == nil {
		m.closeDialog()
		return
	}
	sc := m.ext().scope(m.dform.Values()[0])
	m.closeDialog()
	m.startPreview(p.Source, sc)
}

func (m *Model) submitServerURL() {
	v := m.dform.Values()
	name := strings.TrimSpace(v[0])
	srv, err := mcpserver.HTTPServer(name, strings.TrimSpace(v[1]))
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	if err := install.AddServer(m.ext().scope(v[2]), srv, srv.URL); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.signInInstalled([]string{name})
	m.openAssign(assign.Item{Kind: assign.KindServer, Name: name}, nil)
}

func (m *Model) submitServerSnippet() {
	v := m.dform.Values()
	servers, err := install.ParseSnippet(v[0])
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	sc := m.ext().scope(v[1])
	var items []assign.Item
	for _, s := range servers {
		if err := install.AddServer(sc, s, "pasted"); err != nil {
			m.dform.SetError(err.Error())
			return
		}
		items = append(items, assign.Item{Kind: assign.KindServer, Name: s.Slug})
	}
	m.closeDialog()
	var names []string
	for _, s := range servers {
		names = append(names, s.Slug)
	}
	m.signInInstalled(names)
	m.openAssign(items[0], items[1:])
}

func (m *Model) removeExtension(kind, target string) {
	e := m.ext()
	scopeName, name, _ := strings.Cut(target, "|")
	var sc install.Scope
	switch {
	case e == nil:
		m.closeDialog()
		m.flash = "failed: installs are unavailable here"
		return
	case kind == "skill":
		sc = e.Workspace
		if lib := m.libStore(); lib != nil && lib.Source("skill", target) == library.SourceYours {
			sc = e.User
		}
		name = target
	case scopeName == mcpserver.SourceUser:
		sc = e.User
	default:
		sc = e.Workspace
	}
	// Take it from every bot first, so no manifest points at nothing.
	if m.d.WS != nil && m.d.Org != nil {
		if _, _, err := assign.Apply(m.d.WS, m.d.Org, assign.Item{Kind: kind, Name: name}, nil); err != nil {
			m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
			return
		}
	}
	if err := install.Remove(sc, kind, name); err != nil {
		m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
		return
	}
	m.closeDialog()
	m.reloadLib()
	m.afterCrewWrite(nil, "removed "+name, "")
}

func (m *Model) allowScriptsConfirmed(target string) {
	scopeName, name, _ := strings.Cut(target, "|")
	sc := m.ext().scope(scopeName)
	if scopeName == m.ext().User.Name {
		sc = m.ext().User
	}
	if err := install.AllowScripts(sc, name); err != nil {
		m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
		return
	}
	m.closeDialog()
	m.flash = name + "'s scripts can run now"
}

// ---- shared ----

// roster is the active roster (nil without a workspace).
func (m *Model) roster() ([]*manifest.Agent, error) {
	if m.d.WS == nil {
		return nil, nil
	}
	return org.LoadRoster(m.d.WS)
}

// allBotsPolicy merges both scopes' all-bots sets.
func (m *Model) allBotsPolicy() assign.Policy {
	e := m.ext()
	if e == nil {
		return assign.Policy{}
	}
	var locks []*install.Lock
	for _, sc := range []install.Scope{e.Workspace, e.User} {
		if sc.LockPath == "" {
			continue
		}
		if l, err := install.ReadLock(sc.LockPath); err == nil {
			locks = append(locks, l)
		}
	}
	return assign.PolicyFrom(locks...)
}

// openAgentWith opens the new-agent form with a skill or server already
// in it ("new bot with this skill").
func (m *Model) openAgentWith(field, value string) {
	if m.d.Org == nil || m.d.WS == nil {
		m.flash = "failed: agent management unavailable"
		return
	}
	m.openNewAgentForm()
	idx := 7 // skills
	if field == "tools" {
		idx = 5
	}
	m.form.f.Fields[idx] = kit.NewTextField(m.form.f.Fields[idx].Label, value)
}

func (m *Model) submitSignInPaste() {
	for _, srv := range m.servers() {
		if srv.Slug == m.dtarget {
			if err := m.ext().PasteSignIn(srv, m.dform.Values()[0]); err != nil {
				m.dform.SetError(err.Error())
				return
			}
		}
	}
	m.closeDialog()
	m.flash = "finishing the sign-in…"
}

// signInInstalled opens the browser sign-in for servers just added.
func (m *Model) signInInstalled(names []string) {
	e := m.ext()
	if e == nil || e.SignInNow == nil || len(names) == 0 {
		return
	}
	var servers []mcpserver.Server
	for _, s := range m.servers() {
		for _, n := range names {
			if s.Slug == n {
				servers = append(servers, s)
			}
		}
	}
	e.SignInNow(servers)
}

// listRows is how many list rows fit under a section's header lines.
func (m *Model) listRows(chrome int) int { return max(m.height-2-chrome-2, 3) }

// window is the [from, to) slice of n rows that keeps cur visible in rows.
func window(n, cur, rows int) (int, int) {
	if n <= rows {
		return 0, n
	}
	rows = max(rows-2, 1) // room for the "more" lines
	from := min(max(cur-rows/2, 0), n-rows)
	return from, from + rows
}

// reloadLib drops the library snapshot so the next read sees an install.
func (m *Model) reloadLib() { m.lib, m.d.Library = nil, nil }
