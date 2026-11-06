package clirun

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func adapter(t *testing.T, name string) *CLI {
	t.Helper()
	c, ok := NewRegistry(nil).Get(name)
	if !ok {
		t.Fatalf("no adapter %q", name)
	}
	return c
}

func hasPair(args []string, k, v string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == k && args[i+1] == v {
			return true
		}
	}
	return false
}

// TestClaudeDenyWebInEveryArgvBranch pins F-123: the flag rides the argv
// delivery, the stdin delivery and a resumed session alike.
func TestClaudeDenyWebInEveryArgvBranch(t *testing.T) {
	c := adapter(t, "claude")
	for name, in := range map[string]RunInput{
		"argv":   {Prompt: "p", System: "s"},
		"stdin":  {Stdin: "big"},
		"resume": {Prompt: "p", Resume: "sess-1"},
	} {
		on := in
		on.DenyWeb = true
		if !hasPair(c.BuildArgs(on), "--disallowedTools", "WebFetch,WebSearch") {
			t.Errorf("%s: deny flag missing: %v", name, c.BuildArgs(on))
		}
		if slices.Contains(c.BuildArgs(in), "--disallowedTools") {
			t.Errorf("%s: flag present without DenyWeb", name)
		}
	}
}

func TestCodexDenyWebFreshAndResumedKeepPromptLast(t *testing.T) {
	c := adapter(t, "codex")
	for name, in := range map[string]RunInput{
		"fresh":  {Prompt: "do it", Workdir: "/w"},
		"resume": {Prompt: "do it", Workdir: "/w", Resume: "sess-1"},
	} {
		on := in
		on.DenyWeb = true
		args := c.BuildArgs(on)
		if !hasPair(args, "-c", "web_search=disabled") {
			t.Errorf("%s: deny missing: %v", name, args)
		}
		if last := args[len(args)-1]; !strings.Contains(last, "do it") {
			t.Errorf("%s: prompt is no longer the last arg: %v", name, args)
		}
		for _, a := range c.BuildArgs(in) {
			if strings.Contains(a, "web_search") {
				t.Errorf("%s: web_search set without DenyWeb: %v", name, a)
			}
		}
	}
}

func TestCopilotExcludesItsFetchTools(t *testing.T) {
	c := adapter(t, "copilot")
	on := c.BuildArgs(RunInput{Prompt: "p", DenyWeb: true, MCPURL: "http://127.0.0.1:1/mcp"})
	if !slices.Contains(on, "--excluded-tools=web_fetch,fetch_copilot_cli_documentation") {
		t.Fatalf("exclusion missing: %v", on)
	}
	for _, a := range c.BuildArgs(RunInput{Prompt: "p"}) {
		if strings.HasPrefix(a, "--excluded-tools") {
			t.Fatalf("excluded without DenyWeb: %v", a)
		}
	}
}

func TestAntigravityHookCommandAnswersWithAValidDeny(t *testing.T) {
	body, err := antigravityDenyWebHooks(nil)
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]struct {
		PreToolUse []struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
	}
	if err := json.Unmarshal(body, &all); err != nil {
		t.Fatal(err)
	}
	h := all[antigravityDenyHook].PreToolUse[0]
	if h.Matcher != antigravityWebTools || !strings.Contains(h.Matcher, "search_web") {
		t.Fatalf("matcher = %q", h.Matcher)
	}
	out, err := exec.Command("sh", "-c", h.Hooks[0].Command).Output()
	if err != nil {
		t.Fatal(err)
	}
	var reply struct{ Decision, Reason string }
	if err := json.Unmarshal(out, &reply); err != nil || reply.Decision != "deny" || reply.Reason == "" {
		t.Fatalf("hook output %q -> %+v (%v)", out, reply, err)
	}
}

func TestOpencodeDeniesThroughEnv(t *testing.T) {
	c := adapter(t, "opencode")
	if len(c.DenyWebEnv) != 1 || !strings.HasPrefix(c.DenyWebEnv[0], "OPENCODE_PERMISSION=") ||
		!strings.Contains(c.DenyWebEnv[0], `"webfetch":"deny"`) {
		t.Fatalf("DenyWebEnv = %v", c.DenyWebEnv)
	}
}

// TestWebDenyOKMatchesAMechanism keeps the declaration honest: an adapter
// that claims it can deny must change its argv or carry env, and one that
// does not claim it must do neither.
func TestWebDenyOKMatchesAMechanism(t *testing.T) {
	for _, c := range NewRegistry(nil).All() {
		in := RunInput{Prompt: "p", Workdir: "/w", System: "s", MCPURL: "http://127.0.0.1:1/mcp"}
		on := in
		on.DenyWeb = true
		changes := !slices.Equal(c.BuildArgs(in), c.BuildArgs(on)) || len(c.DenyWebEnv) > 0 || c.DenyWebDir
		if c.WebDenyOK != changes {
			t.Errorf("%s: WebDenyOK=%v but the argv/env mechanism exists=%v", c.Name, c.WebDenyOK, changes)
		}
		if !c.WebDenyOK && c.WebDenyWhy == "" {
			t.Errorf("%s cannot deny native web tools and does not say why", c.Name)
		}
	}
}

func fakeGemini(t *testing.T, userHooks string) string {
	t.Helper()
	real := t.TempDir()
	if err := os.MkdirAll(filepath.Join(real, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if userHooks != "" {
		if err := os.WriteFile(filepath.Join(real, "config", "hooks.json"), []byte(userHooks), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return real
}

func TestAntigravityDenyWebHookIsMergedNotEdited(t *testing.T) {
	user := `{"mine":{"PostToolUse":[{"matcher":"*","hooks":[{"command":"true"}]}]}}`
	real := fakeGemini(t, user)
	dir, cleanup, err := antigravityMCPGeminiDir(real, t.TempDir(), "{}", true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	body, err := os.ReadFile(filepath.Join(dir, "config", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["mine"]; !ok {
		t.Fatalf("the user's hook was lost: %s", body)
	}
	hook := string(got[antigravityDenyHook])
	for _, want := range []string{"PreToolUse", "search_web", `\"decision\":\"deny\"`} {
		if !strings.Contains(hook, want) {
			t.Fatalf("deny hook lacks %q: %s", want, hook)
		}
	}
	// The user's own file is not touched.
	if b, _ := os.ReadFile(filepath.Join(real, "config", "hooks.json")); string(b) != user {
		t.Fatalf("user hooks.json changed: %s", b)
	}
	// Without denial the file is not written (the user's is linked, if any).
	dir2, cleanup2, err := antigravityMCPGeminiDir(fakeGemini(t, ""), t.TempDir(), "{}", false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if _, err := os.Stat(filepath.Join(dir2, "config", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("hooks.json written without denial: %v", err)
	}
}

func TestAntigravityDenyWebRefusesAnUnreadableUserHooksFile(t *testing.T) {
	_, cleanup, err := antigravityMCPGeminiDir(fakeGemini(t, "[not an object"), t.TempDir(), "{}", true)
	if err == nil {
		cleanup()
		t.Fatal("a broken user hooks.json was overwritten")
	}
	if !strings.Contains(err.Error(), "hooks.json") {
		t.Fatalf("error does not name the file: %v", err)
	}
}
