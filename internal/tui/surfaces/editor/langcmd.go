package editor

import (
	"strings"
)

// lspCommand handles :lsp [status] and :lsp install <lang> (F-050, F-110).
// Installing raises the install card: the exact package-manager command,
// run only on yes, in a terminal tab.
func (m *Model) lspCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "lsp" {
		return "", false
	}
	if len(f) == 1 || f[1] == "status" {
		return m.lspStatus(), true
	}
	if f[1] != "install" {
		return "lsp: use :lsp or :lsp install <language>", true
	}
	if len(f) < 3 {
		return "lsp install: name a language — " + m.installableNames(), true
	}
	l, ok := m.langs.ByID(f[2])
	if !ok {
		return "lsp install: unknown language " + f[2] + " — " + m.installableNames(), true
	}
	if l.Install.Tool == "" {
		return l.Name + ": DHI does not install this server — set editor.languages." + l.ID + ".command", true
	}
	if m.lspMgr != nil {
		if _, found := m.lspMgr.Locate(l.Server); found {
			return l.Name + " server is already installed", true
		}
	}
	if !m.askInstall(l.Install.Tool) {
		return "lsp install: unavailable here", true
	}
	return "", true
}

func (m *Model) installableNames() string {
	var ids []string
	for _, l := range m.langs.All() {
		if l.Install.Tool != "" {
			ids = append(ids, l.ID)
		}
	}
	return strings.Join(ids, ", ")
}

// lspStatus summarises each language: running, installed, or not.
func (m *Model) lspStatus() string {
	if m.lspMgr == nil {
		return "lsp: not available"
	}
	var parts []string
	for _, l := range m.langs.All() {
		state := "not installed"
		switch {
		case m.lspMgr.ClientFor(l.ID) != nil:
			state = "running"
		case func() bool { _, ok := m.lspMgr.Locate(l.Server); return ok }():
			state = "installed"
		}
		parts = append(parts, l.ID+": "+state)
	}
	return strings.Join(parts, " · ")
}
