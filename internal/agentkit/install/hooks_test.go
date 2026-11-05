package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/hooks"
)

// fakeHookPlugin writes a vendor plugin that ships a skill and hooks.
func fakeHookPlugin(t *testing.T, inline bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "guardian")
	write := func(rel, body string, mode os.FileMode) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	const hj = `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/guard.sh","timeout":5},{"type":"prompt","command":"x"}]}],"SubagentStop":[{"hooks":[{"type":"command","command":"y"}]}]}}`
	if inline {
		write(".claude-plugin/plugin.json", `{"name":"guardian","description":"policy","hooks":`+
			`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/guard.sh","timeout":5}]}]}}`, 0o644)
	} else {
		write(".claude-plugin/plugin.json", `{"name":"guardian","description":"policy"}`, 0o644)
		write("hooks/hooks.json", hj, 0o644)
	}
	write("scripts/guard.sh", "#!/bin/sh\nexit 0\n", 0o755)
	write("skills/careful/SKILL.md", "---\nname: careful\ndescription: be careful\n---\nbody\n", 0o644)
	return dir
}

func hookScope(t *testing.T) Scope {
	sc := scope(t)
	sc.HooksDir = filepath.Join(filepath.Dir(sc.LockPath), "hooks")
	return sc
}

func TestPluginHooksAreParsedNotUnused(t *testing.T) {
	for _, inline := range []bool{false, true} {
		p, err := ReadPlugin(fakeHookPlugin(t, inline))
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Hooks.Hooks) != 1 || p.Hooks.Hooks[0].Event != hooks.PreToolUse || p.Hooks.Hooks[0].Matcher != "Bash" {
			t.Errorf("inline=%v hooks = %+v", inline, p.Hooks)
		}
		for _, u := range p.Unused {
			if u == "hooks" {
				t.Errorf("inline=%v: hooks must no longer be reported as unused", inline)
			}
		}
		if !inline && len(p.Hooks.Unsupported) != 2 {
			t.Errorf("a prompt-type hook and an unknown event are listed, not run: %+v", p.Hooks.Unsupported)
		}
	}
}

func TestInstallStoresHooksOffThenEnableRunsOnlyUnchanged(t *testing.T) {
	src, _ := ParseSource(fakeHookPlugin(t, false))
	b, err := Preview(context.Background(), src, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	sc := hookScope(t)
	res, err := Apply(b, sc, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hooks != "guardian" {
		t.Fatalf("result = %+v", res)
	}
	// The whole plugin folder is kept, scripts keep their execute bit (the
	// user enables the hooks knowing the commands), and nothing is on yet.
	script := filepath.Join(sc.HooksDir, "guardian", "scripts", "guard.sh")
	if fi, err := os.Stat(script); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("script not stored runnable: %v", err)
	}
	if on, problems := EnabledHooks(sc); len(on) != 0 || len(problems) != 0 {
		t.Fatalf("hooks must be off until enabled: %+v %v", on, problems)
	}
	list, _ := InstalledHooks(sc)
	if len(list) != 1 || list[0].Enabled || len(list[0].Set.Hooks) != 1 {
		t.Fatalf("list = %+v", list)
	}

	if err := SetHooksEnabled(sc, "guardian", true); err != nil {
		t.Fatal(err)
	}
	on, problems := EnabledHooks(sc)
	if len(on) != 1 || len(problems) != 0 || on[0].Name != "guardian" || on[0].Dir != filepath.Join(sc.HooksDir, "guardian") {
		t.Fatalf("enabled = %+v %v", on, problems)
	}
	if !strings.Contains(on[0].Set.Hooks[0].Command, "guard.sh") {
		t.Errorf("command = %q", on[0].Set.Hooks[0].Command)
	}

	// Edited after install: refused by name, never run.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nrm -rf ~\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	on, problems = EnabledHooks(sc)
	if len(on) != 0 || len(problems) != 1 || !strings.Contains(problems[0], "changed since install") {
		t.Fatalf("tampered copy ran: %+v %v", on, problems)
	}
	if err := SetHooksEnabled(sc, "guardian", true); err == nil {
		t.Error("enabling a changed copy must fail")
	}
	if err := SetHooksEnabled(sc, "guardian", false); err != nil {
		t.Errorf("turning off must always work: %v", err)
	}

	// Reinstalling resets to off (the new content is reviewed again).
	if _, err := Apply(b, sc, nil, false); err != nil {
		t.Fatal(err)
	}
	if l, _ := InstalledHooks(sc); l[0].Enabled {
		t.Error("an update must not keep hooks enabled")
	}

	if err := Remove(sc, "hooks", "guardian"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sc.HooksDir, "guardian")); err == nil {
		t.Error("remove left the folder")
	}
	if l, _ := InstalledHooks(sc); len(l) != 0 {
		t.Errorf("lock still lists %+v", l)
	}
}

func TestPickWithoutHooksStoresNone(t *testing.T) {
	src, _ := ParseSource(fakeHookPlugin(t, false))
	b, err := Preview(context.Background(), src, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	sc := hookScope(t)
	res, err := Apply(b, sc, &Pick{Skills: []string{"careful"}}, false)
	if err != nil || res.Hooks != "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(sc.HooksDir); err == nil {
		t.Error("hooks folder written though hooks were not picked")
	}
}
