package settings

// SettingProposal is an agent's proposed settings change (F-070).
type SettingProposal struct {
	ID, Key, Old, New, Agent, Rationale string
}

func (sp SettingProposal) lines() []string {
	return []string{sp.Key + ": " + sp.Old + " → " + sp.New}
}

// pendingSettings lists proposals awaiting review (F-070 fills this in).
func (m *Model) pendingSettings() []SettingProposal { return nil }

func (m *Model) applySetting(SettingProposal)  {}
func (m *Model) rejectSetting(SettingProposal) {}
