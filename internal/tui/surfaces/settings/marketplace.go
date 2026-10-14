package settings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/agentkit/registry"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- MARKETPLACE section (F-034 Part B): signed registry browse/install ----

// marketplaceEntries returns the cached index filtered by the query.
func (m *Model) marketplaceEntries() ([]registry.Entry, error) {
	if m.d.Registry == nil {
		return nil, fmt.Errorf("registry unavailable")
	}
	return m.d.Registry.Search(m.mktQuery)
}

func (m *Model) marketplaceKey(key string) bool {
	if m.mktEdit {
		switch key {
		case "esc", "enter":
			m.mktEdit = false
		case "backspace":
			if len(m.mktQuery) > 0 {
				m.mktQuery = m.mktQuery[:len(m.mktQuery)-1]
			}
		default:
			if r := []rune(key); len(r) == 1 && r[0] >= 32 {
				m.mktQuery += key
			}
		}
		return true
	}
	entries, _ := m.marketplaceEntries()
	switch key {
	case "j", "down":
		if m.mktCur < len(entries)-1 {
			m.mktCur++
			m.flash = ""
		}
	case "k", "up":
		if m.mktCur > 0 {
			m.mktCur--
			m.flash = ""
		}
	case "/", "f":
		m.mktEdit = true
	case "r":
		if m.d.Registry == nil {
			m.flash = "failed: registry unavailable"
			return true
		}
		src, _ := m.d.Registry.Source()
		m.openFormDialog("registry source", dlgRegistrySource, "",
			append([]kit.Field{kit.NewTextField("source", src)}, m.registryScopeField()...)...)
	case "enter", "i":
		if m.mktCur < len(entries) {
			e := entries[m.mktCur]
			m.openConfirmDialog("install pack",
				"install "+e.Name+" (digest-verified against the index)?",
				e.Name, dlgRegistryInstall)
		}
	case "v":
		if m.mktCur < len(entries) {
			m.showMarketplaceEntry(entries[m.mktCur])
		}
	case "t":
		if m.d.Registry == nil {
			m.flash = "failed: registry unavailable"
			return true
		}
		m.openFormDialog("publisher key", dlgRegistryTrustKey, "",
			append([]kit.Field{kit.NewTextField("ed25519 public key (hex)", "")}, m.registryScopeField()...)...)
	case "T":
		if m.d.Registry == nil {
			m.flash = "failed: registry unavailable"
			return true
		}
		if keys, _ := m.d.Registry.TrustedKeys(); len(keys) > 0 {
			m.openConfirmDialog("clear trusted keys",
				"unpin all "+kit.Count(len(keys), "publisher key")+"? future refreshes accept unsigned indexes",
				"", dlgRegistryUntrust)
		} else {
			m.flash = "no publisher keys pinned"
		}
	default:
		return false
	}
	return true
}

var registryScopes = []string{"this workspace", "every workspace"}

// registryScopeField offers your registry (every workspace, F-091) when
// there is one.
func (m *Model) registryScopeField() []kit.Field {
	if m.d.Registry == nil || m.d.Registry.Parent() == nil {
		return nil
	}
	return []kit.Field{kit.NewToggleField("for", registryScopes, 0)}
}

// registryFor picks the registry a dialog's "for" toggle chose.
func (m *Model) registryFor(vals []string) (*registry.Registry, string) {
	if len(vals) > 1 && vals[1] == registryScopes[1] && m.d.Registry.Parent() != nil {
		return m.d.Registry.Parent(), registryScopes[1]
	}
	return m.d.Registry, registryScopes[0]
}

// submitRegistryTrustKey pins a publisher public key so refreshes require
// a verifying index signature.
func (m *Model) submitRegistryTrustKey() {
	f := m.dform
	if m.d.Registry == nil {
		f.SetError("registry unavailable")
		return
	}
	key := strings.TrimSpace(f.Values()[0])
	reg, where := m.registryFor(f.Values())
	if err := reg.TrustKey(key); err != nil {
		f.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.flash = "publisher key pinned for " + where + " — refreshes now require a valid index signature"
}

// submitRegistrySource kicks an async refresh; the cache lands before the
// outcome event, so the next render shows the new entries.
func (m *Model) submitRegistrySource() {
	f := m.dform
	if m.d.Registry == nil {
		f.SetError("registry unavailable")
		return
	}
	src := strings.TrimSpace(f.Values()[0])
	if src == "" {
		f.SetError("registry source (git URL or path) required")
		return
	}
	f.Busy = true
	f.Err = ""
	reg, _ := m.registryFor(f.Values())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		ev := settingsEvent{}
		if err := reg.Refresh(ctx, src); err != nil {
			ev.err = err.Error()
		} else {
			entries, _ := reg.Browse()
			ev.msg = "registry refreshed (" + itoa(len(entries)) + " packs)"
		}
		select {
		case m.events <- ev:
		default:
		}
	}()
}

// submitRegistryInstall verifies the digest and installs asynchronously.
func (m *Model) submitRegistryInstall(name string) {
	m.closeDialog()
	if m.d.Registry == nil || m.d.WS == nil {
		m.flash = "failed: registry unavailable"
		return
	}
	m.flash = ""
	reg := m.d.Registry
	installer := &pack.Installer{WS: m.d.WS}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		ev := settingsEvent{}
		if res, err := reg.Install(ctx, name, installer); err != nil {
			ev.err = err.Error()
		} else {
			ev.msg = "installed pack " + res.Pack + " (" + resultSummary(res) + ")"
		}
		select {
		case m.events <- ev:
		default:
		}
	}()
}

func (m *Model) showMarketplaceEntry(e registry.Entry) {
	lines := []string{
		theme.TextDim().Render("name     ") + e.Name,
		theme.TextDim().Render("version  ") + orDash(e.Version),
		theme.TextDim().Render("source   ") + e.Source,
		theme.TextDim().Render("sha256   ") + e.SHA256,
		"",
	}
	if e.Description != "" {
		lines = append(lines, theme.TextDim().Render(e.Description))
	}
	m.openDisplayDialog("pack "+e.Name, lines)
}

func (m *Model) marketplaceView(w int) []string {
	if m.d.Registry == nil {
		return []string{theme.TextDim().Render(
			"(marketplace unavailable — not inside a workspace)")}
	}
	eff := m.d.Registry.Effective()
	src, hasSrc := eff.Source()
	yours := ""
	if eff != m.d.Registry {
		yours = "  · yours, every workspace"
	}
	var head string
	if at, ok := eff.FetchedAt(); ok {
		head = theme.Hint().Render("registry") + theme.TextDim().Render(
			"  "+orDash(src)+"  · fetched "+humanAge(m.now(), at)+yours)
	} else if hasSrc {
		head = theme.Hint().Render("registry") + theme.TextDim().Render("  "+src+"  (not fetched)")
	} else {
		head = theme.TextDim().Render("(no registry — \"r\" to set a git URL or path)")
	}
	out := []string{head}
	if keys := m.d.Registry.AllTrustedKeys(); len(keys) > 0 {
		out = append(out, theme.TextDim().Render(
			"trust: "+kit.Count(len(keys), "publisher key")+" pinned — signed index required (t pin · T clear)"))
	} else {
		out = append(out, theme.TextDim().Render(
			"trust: digest-only — t to pin a publisher key"))
	}
	if m.mktEdit {
		out = append(out, theme.TabActive().Render("/ "+m.mktQuery)+"▏")
	} else if m.mktQuery != "" {
		out = append(out, theme.TextDim().Render("filter: "+m.mktQuery))
	}

	entries, err := m.marketplaceEntries()
	if err != nil {
		out = append(out, theme.TextDim().Render("("+err.Error()+")"))
		return out
	}
	if len(entries) == 0 {
		out = append(out, theme.TextDim().Render("(no packs match)"))
		return out
	}
	clampCursor(&m.mktCur, len(entries))
	for i, e := range entries {
		line := padTo(cropStr(e.Name, 18), 20) +
			theme.Hint().Render(padTo(orDash(e.Version), 8)) +
			theme.TextMuted().Render(padTo(cropStr(e.SHA256, 10), 12)) +
			theme.TextDim().Render(cropStr(e.Description, maxInt(w-46, 12)))
		if i == m.mktCur {
			out = append(out, theme.GlyphCursor+" "+theme.TabActive().Render(line))
		} else {
			out = append(out, "  "+theme.TextDim().Render(line))
		}
	}
	return out
}

// humanAge renders a rough age for the freshness line.
func humanAge(now, at time.Time) string {
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	default:
		return itoa(int(d.Hours()/24)) + "d ago"
	}
}

func cropStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n || n <= 1 {
		return s
	}
	return string(r[:n-1]) + "…"
}

func clampCursor(c *int, n int) {
	if *c >= n {
		*c = maxInt(n-1, 0)
	}
	if *c < 0 {
		*c = 0
	}
}
