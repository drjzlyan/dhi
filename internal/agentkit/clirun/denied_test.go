package clirun

import "testing"

func TestDeniedPath(t *testing.T) {
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
	} {
		if got := DeniedPath(c.in); got != c.want {
			t.Errorf("DeniedPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
