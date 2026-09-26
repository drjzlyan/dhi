package clirun

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// stubLook resolves names against a single directory (test PATH).
func stubLook(dir string) func(string) (string, error) {
	return func(name string) (string, error) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
		return p, nil
	}
}

// writeStub drops an executable shell script named bin in dir.
func writeStub(t *testing.T, dir, bin, script string) string {
	t.Helper()
	p := filepath.Join(dir, bin)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

const claudeStub = `#!/bin/sh
for a in "$@"; do
  if [ "$a" = "--version" ]; then echo "2.1.177 (Claude Code)"; exit 0; fi
done
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Working on it"}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","id":"t1","input":{"command":"ls -la"}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"file1"}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"boom"}]}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Done: two files.","total_cost_usd":0.0123,"usage":{"input_tokens":100,"output_tokens":50}}'
`

func TestRegistryBasics(t *testing.T) {
	r := NewRegistry(nil)
	names := r.Names()
	wantNames := []string{"antigravity", "claude", "codex", "copilot", "cursor-agent", "opencode"}
	if len(names) != len(wantNames) {
		t.Fatalf("Names = %v, want %v", names, wantNames)
	}
	for i := range wantNames {
		if names[i] != wantNames[i] {
			t.Fatalf("Names = %v, want %v", names, wantNames)
		}
	}
	if c, ok := r.Get("claude"); !ok || c.Bin != "claude" || c.Tested == "" {
		t.Fatalf("Get(claude) = %+v ok=%v", c, ok)
	}
	if c, ok := r.Get("codex"); !ok || c.Bin != "codex" || c.Tested == "" {
		t.Fatalf("Get(codex) = %+v ok=%v", c, ok)
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("Get(unknown) must miss")
	}
	for name, want := range map[string]bool{
		"": false, "anthropic": false, "claude": true, "codex": true,
		"opencode": true, "cursor-agent": true, "copilot": true, "antigravity": true,
		"Claude": false, "nope": false,
	} {
		if got := r.ValidRuntime(name); got != want {
			t.Errorf("ValidRuntime(%q) = %v, want %v", name, got, want)
		}
	}
	if !strings.Contains(r.ValidRuntimeNames(), "claude") {
		t.Errorf("ValidRuntimeNames missing claude: %s", r.ValidRuntimeNames())
	}
	all := r.All()
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	for _, c := range all {
		if c.BuildArgs == nil || c.ParseStream == nil || c.Finalize == nil || c.Version == nil {
			t.Errorf("adapter %s has nil function fields", c.Name)
		}
		if len(c.EnvPass) == 0 {
			t.Errorf("adapter %s declares no env pass-through", c.Name)
		}
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "claude", claudeStub)
	r := NewRegistry(stubLook(dir))
	if got := r.Detect()["claude"]; got != "2.1.177" {
		t.Errorf("claude version = %q, want 2.1.177", got)
	}

	// a failing --version probe is "not detected", not an error (ADR-0011)
	bad := t.TempDir()
	writeStub(t, bad, "claude", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then exit 3; fi\n")
	if got := NewRegistry(stubLook(bad)).Detect()["claude"]; got != "" {
		t.Errorf("broken probe = %q, want not detected", got)
	}

	// no binary at all: undetected
	if got := NewRegistry(stubLook(t.TempDir())).Detect()["claude"]; got != "" {
		t.Errorf("missing binary = %q, want not detected", got)
	}
}
