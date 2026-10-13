package settings

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- typed values for CONFIG rows (F-084): nothing needs a file ----

// valueRow is a CONFIG row that takes a typed value: its current text
// and how to set it.
type valueRow struct {
	label string
	cur   string
	set   func(v string) error
	hint  string
}

func atoiIn(v string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("a whole number, please")
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("between %d and %d, please", lo, hi)
	}
	return n, nil
}

func dollars(v string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(v), "$"), 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("an amount like 2.50 (0 = no limit)")
	}
	return f, nil
}

func intRow(label string, dst *int, lo, hi int, hint string) *valueRow {
	return &valueRow{label: label, cur: itoa(*dst), hint: hint, set: func(v string) error {
		n, err := atoiIn(v, lo, hi)
		if err == nil {
			*dst = n
		}
		return err
	}}
}

func usdRow(label string, dst *float64) *valueRow {
	return &valueRow{label: label, cur: strconv.FormatFloat(*dst, 'f', -1, 64), hint: "dollars; 0 = no limit",
		set: func(v string) error {
			f, err := dollars(v)
			if err == nil {
				*dst = f
			}
			return err
		}}
}

func textRow(label string, dst *string, hint string) *valueRow {
	return &valueRow{label: label, cur: *dst, hint: hint, set: func(v string) error {
		*dst = strings.TrimSpace(v)
		return nil
	}}
}

// valueRowAt is the typed editor for the row under the cursor; nil for
// rows that only toggle.
func (m *Model) valueRowAt(row int) *valueRow {
	c := &m.cfg
	switch row {
	case rowRole:
		return textRow("you are a", &c.Profile.Role, "e.g. developer, tech lead, founder")
	case rowLanguage:
		return textRow("language", &c.Profile.Writing.Language, "the language employees write in for you")
	case rowTabWidth:
		return intRow("tab width", &c.Editor.TabWidth, 1, 16, "1–16")
	case rowScrollback:
		return intRow("scrollback", &c.Terminal.Scrollback, 100, 1000000, "lines kept per terminal")
	case rowWorktrees:
		return textRow("worktrees", &c.Worktrees.Root, "a folder for task worktrees; empty keeps them inside .dhi/")
	case rowBranchTask:
		return textRow("branch", &c.Conventions.Branch.Task, "placeholders: {slug} {id} {user} {date} {ticket}")
	case rowCommitCoAuthor:
		cm := &c.Conventions.Commit
		return &valueRow{label: "co-author", cur: cm.CoAuthor, hint: `"Name <email>", added as a Co-Authored-By trailer; empty turns it off`,
			set: func(v string) error {
				cm.CoAuthor = strings.TrimSpace(v)
				cm.CoAuthorEnabled = cm.CoAuthor != ""
				return nil
			}}
	case rowCopyright:
		cp := &c.Conventions.Copyright
		return &valueRow{label: "holder", cur: cp.Holder, hint: "named in new-file headers; empty turns headers off",
			set: func(v string) error {
				cp.Holder = strings.TrimSpace(v)
				cp.Enabled = cp.Holder != ""
				return nil
			}}
	case rowLicense:
		return textRow("license", &c.Conventions.Copyright.License, "an SPDX id such as MIT or Apache-2.0; empty = none")
	case rowMaxSubject:
		return intRow("max subject", &c.Conventions.Commit.MaxSubject, 20, 200, "characters, 20–200")
	case rowEngine:
		return &valueRow{label: "engine", cur: strings.TrimPrefix(c.Engine, "cli:"), hint: "a coding CLI: " + strings.Join(m.d.CLIs, ", "),
			set: func(v string) error {
				if v = strings.TrimSpace(strings.TrimPrefix(v, "cli:")); v == "" {
					c.Engine = ""
				} else {
					c.Engine = "cli:" + v
				}
				return nil
			}}
	case rowMaxTurns:
		return intRow("max turns", &c.Agents.MaxTurns, 0, 500, "0 = the CLI's default")
	case rowMaxIter:
		return intRow("max rounds", &c.Agents.MaxIterations, 1, 10, "verify → fix rounds per task, 1–10")
	case rowStall:
		return intRow("stall secs", &c.Agents.StallSeconds, 0, 7200, "0 = off, else 60–7200")
	case rowReflectModel:
		return textRow("model", &c.Agents.ReflectModel, "the model that reflects on turns; empty = the employee's own")
	case rowTaskUSD:
		return usdRow("per task", &c.Budgets.TaskUSD)
	case rowTaskTokens:
		return intRow("tokens", &c.Budgets.TaskTokens, 0, 100_000_000, "per task; 0 = no limit")
	case rowAgentDayUSD:
		return usdRow("per agent", &c.Budgets.AgentDayUSD)
	case rowWorkDayUSD:
		return usdRow("per day", &c.Budgets.WorkspaceDayUSD)
	case rowMemPrompt:
		return intRow("tokens", &c.Memory.PromptTokens, 4000, 400000, "per prompt, 4000–400000")
	case rowMemSession:
		return intRow("tokens", &c.Memory.SessionTokens, 20000, 2000000, "before a fresh session")
	case rowMemJournal:
		return intRow("entries", &c.Memory.JournalMax, 50, 100000, "journal entries kept per employee")
	case rowMemLessons:
		return intRow("lessons", &c.Memory.LessonsMax, 10, 10000, "active lessons kept")
	case rowMemPinned:
		return intRow("pinned", &c.Memory.PinnedMax, 0, 100, "0 = no cap")
	case rowMemDecay:
		return intRow("days", &c.Memory.DecayDays, 1, 3650, "unused lessons fade after this many days")
	}
	return nil
}

// openValueEditor opens the editor for the row under the cursor: a
// typed value, the language switches, or the key remaps.
func (m *Model) openValueEditor() {
	switch m.cursor {
	case rowLanguages:
		m.openLanguages()
		return
	case rowKeys:
		m.openKeys()
		return
	}
	vr := m.valueRowAt(m.cursor)
	if vr == nil {
		m.flash = "this one switches: enter or ←/→"
		return
	}
	m.openFormDialog(vr.label, dlgConfigValue, "", kit.NewTextField(vr.label, vr.cur))
	m.valueHint = vr.hint
}

// submitValue sets the typed value, checks the whole config, and saves.
func (m *Model) submitValue() {
	before := m.cfg
	vr := m.valueRowAt(m.cursor)
	if vr == nil {
		m.closeDialog()
		return
	}
	if err := vr.set(m.dform.Values()[0]); err != nil {
		m.cfg = before
		m.dform.SetError(err.Error())
		return
	}
	if err := settings.Check(m.cfg); err != nil {
		m.cfg = before
		m.dform.SetError(strings.TrimPrefix(err.Error(), "settings: "))
		return
	}
	m.closeDialog()
	m.applyAndPersist()
}

// ---- editor.languages ----

func (m *Model) languagesValue() string {
	langs := langserver.Builtin()
	on := 0
	for _, l := range langs {
		if m.languageOn(l.ID) {
			on++
		}
	}
	return fmt.Sprintf("%d of %d on", on, len(langs))
}

func (m *Model) languageOn(id string) bool {
	if lc, ok := m.cfg.Editor.Languages[id]; ok && lc.Enabled != nil {
		return *lc.Enabled
	}
	return true
}

func (m *Model) openLanguages() {
	var fields []kit.Field
	for _, l := range langserver.Builtin() {
		i := 0
		if !m.languageOn(l.ID) {
			i = 1
		}
		fields = append(fields, kit.NewToggleField(l.ID, []string{"on", "off"}, i))
	}
	m.openFormDialog("editor languages", dlgLanguages, "", fields...)
}

func (m *Model) submitLanguages() {
	if m.cfg.Editor.Languages == nil {
		m.cfg.Editor.Languages = map[string]settings.LanguageConfig{}
	}
	for i, l := range langserver.Builtin() {
		on := m.dform.Values()[i] == "on"
		lc := m.cfg.Editor.Languages[l.ID]
		if on && lc.Enabled == nil {
			continue // default stays implicit
		}
		lc.Enabled = &on
		m.cfg.Editor.Languages[l.ID] = lc
	}
	m.closeDialog()
	m.applyAndPersist()
}

// ---- keys ----

func (m *Model) openKeys() {
	pairs := make([]string, 0, len(m.cfg.Keys))
	for k, v := range m.cfg.Keys {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	m.openFormDialog("key remaps", dlgKeys, "", kit.NewTextField("remaps", strings.Join(pairs, ", ")))
	m.valueHint = `pressed=meant, comma-separated, e.g. "ctrl+k=ctrl+p, x=d"; empty clears them`
}

func (m *Model) submitKeys() {
	before := m.cfg
	keys := map[string]string{}
	for _, p := range strings.Split(m.dform.Values()[0], ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		from, to, ok := strings.Cut(p, "=")
		if !ok {
			m.dform.SetError(fmt.Sprintf("%q: write pressed=meant", p))
			return
		}
		keys[strings.TrimSpace(from)] = strings.TrimSpace(to)
	}
	m.cfg.Keys = keys
	if err := settings.Check(m.cfg); err != nil {
		m.cfg = before
		m.dform.SetError(strings.TrimPrefix(err.Error(), "settings: "))
		return
	}
	m.closeDialog()
	m.applyAndPersist()
}

// ---- small renderers ----

func (m *Model) engineChoices() []string {
	out := []string{""}
	for _, c := range m.d.CLIs {
		out = append(out, "cli:"+c)
	}
	if m.cfg.Engine != "" && !contains(out, m.cfg.Engine) {
		out = append(out, m.cfg.Engine)
	}
	return out
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func sandboxValue(mode string) string {
	if mode == settings.SandboxOff {
		return theme.WarningText().Render("off — employees run without the sandbox")
	}
	return theme.SuccessText().Render("auto")
}

func copyrightValue(c conventions.Copyright) string {
	if !c.Enabled {
		return theme.TextMuted().Render(theme.GlyphCross + " off — e names the holder")
	}
	return theme.SuccessText().Render(theme.GlyphCheck+" ") + c.Holder
}
