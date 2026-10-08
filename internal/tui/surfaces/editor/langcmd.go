package editor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/langserver"
)

// installTimeout caps one confirmed language-server install.
const installTimeout = 6 * time.Minute

// lspCommand handles :lsp [status] and :lsp install <lang> [yes] (F-050).
// Provisioning is confirm-gated: without `yes` it only prints the plan.
func (m *Model) lspCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "lsp" {
		return "", false
	}
	if len(f) == 1 || f[1] == "status" {
		return m.lspStatus(), true
	}
	if f[1] != "install" {
		return "lsp: use :lsp, :lsp install <language>, or :lsp install <language> yes", true
	}
	if len(f) < 3 {
		return "lsp install: name a language — " + m.installableNames(), true
	}
	l, ok := m.langs.ByID(f[2])
	if !ok {
		return "lsp install: unknown language " + f[2] + " — " + m.installableNames(), true
	}
	switch l.Install.Method {
	case langserver.MethodToolchain:
		return l.Name + ": " + l.Install.Note, true
	case langserver.MethodNone:
		return l.Name + ": DHI does not install this server — set editor.languages." + l.ID + ".command", true
	}
	if m.lspMgr == nil || m.lspInstaller == nil || m.lspMgr.ManagedPrefix(l.ID) == "" {
		return "lsp install: unavailable — the DHI toolchain (npm) is not installed; finish the first-run install", true
	}
	if _, found := m.lspMgr.Locate(l.ID, l.Server); found {
		return l.Name + " server is already installed", true
	}
	prefix := m.lspMgr.ManagedPrefix(l.ID)
	if len(f) < 4 || f[3] != "yes" {
		return fmt.Sprintf(":lsp install %s yes — installs %s with DHI's npm into %s",
			l.ID, strings.Join(l.Install.Packages, " + "), prefix), true
	}
	if m.lspInstalling[l.ID] {
		return l.Name + " install already running", true
	}
	m.lspInstalling[l.ID] = true
	ch, install := m.termMsgs, m.lspInstaller
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		err := install(ctx, prefix, l.Install.Packages)
		msg := teaMsg{kind: lspMsgInstalled, lang: l.ID, installOK: true, note: l.Name + " language server installed"}
		if err != nil {
			msg.installOK, msg.note = false, "lsp install "+l.ID+" failed: "+err.Error()
		}
		ch <- msg
	}()
	return "installing " + strings.Join(l.Install.Packages, " + ") + " … (this takes a minute)", true
}

func (m *Model) installableNames() string {
	var ids []string
	for _, l := range m.langs.All() {
		if l.Install.Method == langserver.MethodNPM {
			ids = append(ids, l.ID)
		}
	}
	return strings.Join(ids, ", ")
}

// lspStatus summarises each language: running, installed, or how to get it.
func (m *Model) lspStatus() string {
	if m.lspMgr == nil {
		return "lsp: not available (no DHI toolchain)"
	}
	var parts []string
	for _, l := range m.langs.All() {
		state := "not installed"
		switch {
		case m.lspMgr.ClientFor(l.ID) != nil:
			state = "running"
		case func() bool { _, ok := m.lspMgr.Locate(l.ID, l.Server); return ok }():
			state = "installed"
		case m.lspInstalling[l.ID]:
			state = "installing"
		}
		parts = append(parts, l.ID+": "+state)
	}
	return strings.Join(parts, " · ")
}
