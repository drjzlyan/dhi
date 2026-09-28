package scopes

import "testing"

func TestDefaultEffects(t *testing.T) {
	d := Default()
	cases := map[Scope]Effect{
		Read: Auto, Write: Ask, Exec: Ask, Git: Ask,
		Push: Ask, Network: Deny, Admin: Deny,
	}
	for sc, want := range cases {
		if got := d.EffectFor(sc); got != want {
			t.Errorf("%s = %q, want %q", sc, got, want)
		}
	}
}

func TestToolScope(t *testing.T) {
	cases := map[string]Scope{
		"read": Read, "list": Read, "glob": Read, "workspace_search": Read,
		"git_status": Read, "git_log": Read, "git_branch": Read, "git_diff": Read,
		"lsp_hover": Read, "lsp_definition": Read, "lsp_references": Read, "lsp_code_action": Read,
		"ideation_list": Read, "ideation_read": Read, "ask_human": Read,
		"task_list": Read, "kb_search": Read, "channel_read": Read,
		"memory_append": Read, "memory_read_notes": Read, "memory_write_notes": Read,
		"write": Write, "patch": Write, "editor_apply_edit": Write,
		"task_create": Write, "task_status": Write, "task_assign": Write,
		"kb_contribute": Write, "channel_post": Write, "lsp_rename": Write,
		"git_commit": Git, "run": Exec,
	}
	for tool, want := range cases {
		if got := ToolScope(tool); got != want {
			t.Errorf("ToolScope(%q) = %q, want %q", tool, got, want)
		}
	}
	if got := ToolScope("unknown_tool"); got != Admin {
		t.Errorf("unknown tool scope = %q, want admin (deny-by-default)", got)
	}
	// Bridged third-party MCP tools are network by default (deny).
	if got := ToolScope("mcp__fs__read"); got != Network {
		t.Errorf("mcp tool scope = %q, want network", got)
	}
}

func TestResolveLayers(t *testing.T) {
	base := Default()
	team := Set{Write: Deny}
	agent := Set{Push: Auto}
	got := Resolve(base, team, agent)
	if got.EffectFor(Write) != Deny {
		t.Errorf("write = %q, want deny", got.EffectFor(Write))
	}
	if got.EffectFor(Push) != Auto {
		t.Errorf("push = %q, want auto", got.EffectFor(Push))
	}
	if got.EffectFor(Read) != Auto {
		t.Errorf("read = %q, want auto (base preserved)", got.EffectFor(Read))
	}
}

func TestParseEffect(t *testing.T) {
	for _, s := range []string{"auto", "ask", "deny"} {
		if _, err := ParseEffect(s); err != nil {
			t.Errorf("ParseEffect(%q): %v", s, err)
		}
	}
	if _, err := ParseEffect("maybe"); err == nil {
		t.Error("bad effect must refuse")
	}
}

func TestParseScope(t *testing.T) {
	for _, s := range []string{"read", "write", "exec", "network", "git", "push", "admin"} {
		if _, err := ParseScope(s); err != nil {
			t.Errorf("ParseScope(%q): %v", s, err)
		}
	}
	if _, err := ParseScope("everything"); err == nil {
		t.Error("bad scope must refuse")
	}
}
