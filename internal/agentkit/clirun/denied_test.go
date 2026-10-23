package clirun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeniedPath(t *testing.T) {
	homeDir = func() (string, error) { return "/home/me", nil }
	defer func() { homeDir = os.UserHomeDir }()
	for _, c := range []struct{ in, want string }{
		// The yume-forge failure, verbatim.
		{"EPERM: operation not permitted, mkdir '/private/tmp/claude-501/-Users-x-yume-forge'", "/private/tmp/claude-501/-Users-x-yume-forge"},
		{"touch: /opt/data/out.txt: Operation not permitted", "/opt/data"},
		{"open /Users/x/cache/a.db: operation not permitted", "/Users/x/cache"},
		{`Error: EPERM: operation not permitted, open "/var/log/app.log"`, "/var/log"},
		// Unix permissions, not the sandbox: no grant would help.
		{"bash: /etc/hosts: Permission denied", ""},
		{"command failed (exit 1): no such file", ""},
		{"EPERM: operation not permitted", ""},
		{"rm: /x: Operation not permitted", ""}, // parent is "/" — never offered
		{"zsh:1: operation not permitted: /dev/null", ""},
		{"fatal: cannot lock ref 'refs/heads/main': Unable to create '/r/.git/refs/heads/main.lock': Operation not permitted", ""},
		// bubblewrap's read-only binds.
		{"touch: cannot touch '/opt/data/out.txt': Read-only file system", "/opt/data"},
		// A file directly in home is granted as the file.
		{"open /home/me/.zshrc: operation not permitted", "/home/me/.zshrc"},
	} {
		if got := DeniedPath(c.in); got != c.want {
			t.Errorf("DeniedPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A program the sandbox would not run is named itself (exec card).
func TestDeniedProgramIsAnExecDenial(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)
	if got := DeniedPath("zsh:1: operation not permitted: " + tool); got != tool {
		t.Fatalf("exec denial = %q", got)
	}
	if got := DeniedPath("open " + tool + ": operation not permitted"); got != dir {
		t.Fatalf("a write to an executable stays a write: %q", got)
	}
}
