package settings

import (
	"fmt"

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
