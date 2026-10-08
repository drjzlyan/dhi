package langserver_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/toolchain"
)

// TestPinnedServersInstallAndHandshake installs every npm-provisioned
// server at its pinned version with DHI's own npm and talks to it. It needs
// the network and the DHI toolchain, so it runs only with DHI_SMOKE_LSP=1.
func TestPinnedServersInstallAndHandshake(t *testing.T) {
	if os.Getenv("DHI_SMOKE_LSP") == "" {
		t.Skip("set DHI_SMOKE_LSP=1 to install and handshake the pinned servers (network)")
	}
	root, err := toolchain.DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	mgr := toolchain.New(root)
	env := mgr.Env(nil)
	samples := map[string]struct{ file, text string }{
		"typescript": {"a.ts", "const x: number = 1;\n"},
		"python":     {"a.py", "x: int = 1\n"},
		"bash":       {"a.sh", "#!/bin/bash\necho hi\n"},
		"yaml":       {"a.yaml", "a: 1\n"},
		"json":       {"a.json", "{\"a\": 1}\n"},
	}
	for _, l := range langserver.Builtin() {
		if l.Install.Method != langserver.MethodNPM {
			continue
		}
		l := l
		t.Run(l.ID, func(t *testing.T) {
			prefix := t.TempDir()
			bin, err := mgr.NPMInstall(context.Background(), prefix, l.Install.Packages[0], l.Install.Packages[1:]...)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			s := samples[l.ID]
			path := filepath.Join(dir, s.file)
			if err := os.WriteFile(path, []byte(s.text), 0o644); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			c, err := lsp.StartProcess(ctx, filepath.Join(bin, l.Server), l.Args, env, dir)
			if err != nil {
				t.Fatalf("%s did not start: %v", l.Server, err)
			}
			defer c.Shutdown()
			_, langID, _ := mustFor(t, l, s.file)
			if err := c.DidOpen(path, s.text, langID); err != nil {
				t.Fatal(err)
			}
			if _, err := c.DocumentSymbols(path); err != nil {
				t.Fatalf("documentSymbol: %v", err)
			}
		})
	}
}

func mustFor(t *testing.T, l langserver.Language, file string) (langserver.Language, string, bool) {
	t.Helper()
	r, _ := langserver.Resolve(nil)
	got, id, ok := r.For(file)
	if !ok || got.ID != l.ID {
		t.Fatalf("%s resolves to %q", file, got.ID)
	}
	return got, id, ok
}
