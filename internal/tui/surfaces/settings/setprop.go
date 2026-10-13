package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/setprop"
	dhisettings "github.com/drjzlyan/dhi/internal/settings"
)

// SettingProposal is an agent's proposed settings change (F-070) as the
// PROPOSALS section shows it.
type SettingProposal struct {
	ID, Key, Old, New, Agent, Rationale, Layer string
}

func (sp SettingProposal) lines() []string {
	old := sp.Old
	if old == "" {
		old = "(unset)"
	}
	if sp.Layer == setprop.LayerProject {
		out := []string{"project brief · " + strings.TrimPrefix(sp.Key, "project."), "  now:"}
		for _, l := range strings.Split(old, "\n") {
			out = append(out, "    "+l)
		}
		out = append(out, "  proposed:")
		for _, l := range strings.Split(sp.New, "\n") {
			out = append(out, "    "+l)
		}
		return append(out, "  proposed by "+sp.Agent, "", "why: "+orDash(sp.Rationale), "",
			"y replaces this one field of the brief; nothing else changes.")
	}
	return []string{
		sp.Key,
		fmt.Sprintf("  %s → %s", old, sp.New),
		fmt.Sprintf("  layer: %s config · proposed by %s", sp.Layer, sp.Agent),
		"", "why: " + orDash(sp.Rationale), "",
		"y applies exactly this one key; nothing else in the file changes.",
	}
}

// pendingSettings lists the agents' pending settings proposals.
func (m *Model) pendingSettings() []SettingProposal {
	if m.d.SetProps == nil {
		return nil
	}
	ps, err := m.d.SetProps.Pending()
	if err != nil {
		return nil
	}
	out := make([]SettingProposal, 0, len(ps))
	for _, p := range ps {
		out = append(out, SettingProposal{ID: p.ID, Key: p.Key, Old: p.Old, New: p.Value,
			Agent: p.Agent, Rationale: p.Rationale, Layer: p.Layer})
	}
	return out
}

// applySetting writes the one key into the proposal's layer, reloads the
// live config, and records the approval.
func (m *Model) applySetting(sp SettingProposal) {
	if sp.Layer == setprop.LayerProject {
		m.applyBriefProposal(sp)
		return
	}
	path := m.d.WorkspaceConfig
	if sp.Layer == setprop.LayerUser {
		path = m.d.UserConfig
	}
	if path == "" {
		m.flash = "failed: no " + sp.Layer + " config path"
		return
	}
	if err := dhisettings.SetLayerKey(path, sp.Key, sp.New); err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	if err := m.d.SetProps.Resolve(sp.ID, setprop.StatusApproved); err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	if c, err := dhisettings.Load(m.d.UserConfig, m.d.WorkspaceConfig); err == nil {
		m.cfg = c
		m.cfg.Apply()
		m.saved = dhisettings.Snapshot(c)
	}
	m.flash = fmt.Sprintf("applied %s = %s (%s layer) — live from the agents' next spawn", sp.Key, sp.New, sp.Layer)
}

func (m *Model) rejectSetting(sp SettingProposal) {
	if err := m.d.SetProps.Resolve(sp.ID, setprop.StatusRejected); err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	m.flash = "rejected the " + sp.Key + " proposal"
}

// applyBriefProposal replaces one field of the project brief (F-080).
func (m *Model) applyBriefProposal(sp SettingProposal) {
	if m.d.WS == nil {
		m.flash = "failed: not inside a workspace"
		return
	}
	b, _, err := brief.Load(m.d.WS.Root)
	if err == nil {
		err = b.Set(strings.TrimPrefix(sp.Key, "project."), sp.New)
	}
	if err == nil {
		err = brief.Save(m.d.WS.Root, b)
	}
	if err == nil {
		err = m.d.SetProps.Resolve(sp.ID, setprop.StatusApproved)
	}
	if err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	m.flash = "project brief updated: " + strings.TrimPrefix(sp.Key, "project.") + " — employees read it from their next turn"
}
