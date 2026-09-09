// Package stubcli provisions fixture host-CLI companions for tests that
// route agents through the real runtime + CLI registry (F-013,
// ADR-0012). Test doubles live here so surfaces and command wiring test
// the same spawn path production uses, not a fake provider.
package stubcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
)

// Claude writes a fixture `claude` companion into a fresh temp bin dir
// and returns the hermetic CLIEnv (PATH = bin dir + standard system
// dirs, so the script's own helpers resolve) and a registry resolving
// claude to it.
func Claude(t *testing.T, script string) (binDir, cliEnv string, reg *clirun.Registry) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := "PATH=" + dir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin"
	reg = clirun.NewRegistry(func(name string) (string, error) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
		return p, nil
	})
	return dir, env, reg
}

// FixedReply builds a claude stub whose terminal result carries reply
// verbatim. The JSON is assembled in Go and escaped for the shell at
// build time, so the prompt (with newlines and quotes) never leaks into
// invalid JSON the way an echo of "$2" would.
func FixedReply(t *testing.T, reply string) (binDir, cliEnv string, reg *clirun.Registry) {
	t.Helper()
	result, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": reply, "total_cost_usd": 0,
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Single-quote the raw JSON so backticks/fences in replies survive
	// the shell; only "'" needs the escape dance inside single quotes.
	shQuoted := "'" + strings.ReplaceAll(string(result), "'", `'\''`) + "'"
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\"}'\n" +
		fmt.Sprintf("printf '%%s\\n' %s\n", shQuoted) +
		"exit 0\n"
	return Claude(t, script)
}
