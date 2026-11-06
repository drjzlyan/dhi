package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const longHook = "python3 \"${CLAUDE_PLUGIN_ROOT}/scripts/policy_check.py\" --deny-pattern 'rm -rf' --deny-pattern 'curl | sh' --log /var/log/policy.log --strict"

func hookPluginFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "policy")
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "hooks"), 0o755))
	must(os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"policy","description":"guards"}`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "hooks", "hooks.json"), []byte(
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":`+quote(longHook)+`,"timeout":10},{"type":"prompt","command":"ask"}]}]}}`), 0o644))
	return dir
}

func quote(s string) string {
	b := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + b + `"`
}

// F-179: the install card shows each hook command in full, installing keeps
// the hooks off, and INTEGRATIONS (h) turns them on per plugin.
func TestInstallCardShowsHookCommandsAndHooksStartOff(t *testing.T) {
	m, ws := extSurface(t)
	m.d.Ext.Workspace.HooksDir = filepath.Join(ws.Root, workspace.DirHooks)
	for m.sec != secSkills {
		feed(m, "]")
	}
	feed(m, "i")
	m.HandleKey("paste:" + hookPluginFolder(t))
	feed(m, "enter")
	drainDialogEvent(t, m)
	if m.dkind != dlgExtConfirm {
		t.Fatalf("no confirmation (flash %q)", m.flash)
	}
	v := ansi.Strip(m.View())
	flat := strings.Join(strings.Fields(v), " ")
	for _, want := range []string{"PreToolUse [Bash]", "stops after 10s", "off until you turn them on", "not run: PreToolUse prompt"} {
		if !strings.Contains(flat, want) {
			t.Errorf("card lacks %q:\n%s", want, v)
		}
	}
	// Verbatim: the whole command is on the card, wrapped never clipped.
	var inner strings.Builder
	for _, l := range strings.Split(v, "\n") {
		if i, j := strings.Index(l, "│"), strings.LastIndex(l, "│"); i >= 0 && j > i {
			inner.WriteString(l[i+len("│") : j])
		}
	}
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	if !strings.Contains(squash(inner.String()), squash(longHook)) {
		t.Errorf("the command is not shown in full:\n%s", v)
	}
	feed(m, "enter")
	if !strings.Contains(m.flash, "hooks of policy stored, off") {
		t.Fatalf("flash = %q", m.flash)
	}
	if on, _ := install.EnabledHooks(m.d.Ext.Workspace); len(on) != 0 {
		t.Fatal("hooks must be off after install")
	}

	for m.sec != secIntegrations {
		feed(m, "]")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "PLUGIN HOOKS") || !strings.Contains(v, "policy") {
		t.Fatalf("integrations must list the plugin's hooks:\n%s", v)
	}
	feed(m, "h")
	if m.dkind != dlgPluginHooks {
		t.Fatalf("h did not open the hooks dialog (flash %q)", m.flash)
	}
	feed(m, "right", "enter")
	on, problems := install.EnabledHooks(m.d.Ext.Workspace)
	if len(on) != 1 || on[0].Name != "policy" || len(problems) != 0 {
		t.Fatalf("enabled = %+v %v (flash %q)", on, problems, m.flash)
	}
	if !strings.Contains(m.flash, "policy on") {
		t.Errorf("flash = %q", m.flash)
	}
}
