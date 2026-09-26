package dhitools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// memberDir returns the absolute path of a fixture member root.
func memberDir(t *testing.T, ws *workspace.Workspace, name string) string {
	t.Helper()
	m, ok := ws.Member(name)
	if !ok {
		t.Fatalf("fixture has no member %q", name)
	}
	return m.Path
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFSToolsServed confirms the filesystem tools join the served
// surface (one source of truth for runtime + doctor).
func TestFSToolsServed(t *testing.T) {
	for _, s := range []string{"read", "list", "glob"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}

func TestFSReadReturnsMemberFile(t *testing.T) {
	f, m := newFixture(t, "read")
	writeFile(t, memberDir(t, f.ws, "api"), "main.go", "package main\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "read", `{"path":"api/main.go"}`)
	if isErr {
		t.Fatalf("read refused: %s", out)
	}
	if out != "package main\n" {
		t.Fatalf("read = %q", out)
	}
}

func TestFSReadRefusalsNameTheFix(t *testing.T) {
	f, m := newFixture(t, "read")
	writeFile(t, memberDir(t, f.ws, "api"), "main.go", "x\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()

	cases := []struct{ name, args, want string }{
		{"escape", `{"path":"api/../secret"}`, "escape"},
		{"unknown member", `{"path":"nope/main.go"}`, "unknown member"},
		{"missing file", `{"path":"api/missing.go"}`, "missing.go"},
		{"empty path", `{"path":""}`, "path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := call(h, t, "read", tc.args)
			if !isErr {
				t.Fatalf("expected refusal, got %q", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("refusal %q does not name %q", out, tc.want)
			}
		})
	}
}

func TestFSListListsMemberEntries(t *testing.T) {
	f, m := newFixture(t, "list")
	api := memberDir(t, f.ws, "api")
	writeFile(t, api, "main.go", "x\n")
	writeFile(t, api, "pkg/util.go", "y\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "list", `{"path":"api"}`)
	if isErr {
		t.Fatalf("list refused: %s", out)
	}
	// Directories carry a trailing slash; entries are stable-sorted.
	if out != "main.go\npkg/\n" {
		t.Fatalf("list = %q", out)
	}

	sub, isErr := call(h, t, "list", `{"path":"api/pkg"}`)
	if isErr || sub != "util.go\n" {
		t.Fatalf("list api/pkg = %q err=%v", sub, isErr)
	}
}

func TestFSGlobMatchesWithinMembers(t *testing.T) {
	f, m := newFixture(t, "glob")
	api := memberDir(t, f.ws, "api")
	writeFile(t, api, "main.go", "x\n")
	writeFile(t, api, "pkg/util.go", "y\n")
	writeFile(t, api, "README.md", "z\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "glob", `{"pattern":"*.go"}`)
	if isErr {
		t.Fatalf("glob refused: %s", out)
	}
	if out != "api/main.go\n" {
		t.Fatalf("glob *.go = %q", out)
	}

	sub, isErr := call(h, t, "glob", `{"pattern":"pkg/*.go"}`)
	if isErr || sub != "api/pkg/util.go\n" {
		t.Fatalf("glob pkg/*.go = %q err=%v", sub, isErr)
	}
}

func TestFSGlobRefusesEscapePattern(t *testing.T) {
	f, m := newFixture(t, "glob")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "glob", `{"pattern":"../*.go"}`)
	if !isErr || !strings.Contains(out, "..") {
		t.Fatalf("escape pattern must refuse: %q", out)
	}
}

// TestFSAllowlistGates confirms an agent without the fs tools sees none.
func TestFSAllowlistGates(t *testing.T) {
	f, m := newFixture(t, "task_list")
	h := Deps{Agent: m, WS: f.ws, Tasks: f.tasks, Approvals: f.approvals}.Handler()
	for _, name := range toolNames(h) {
		switch name {
		case "read", "list", "glob":
			t.Fatalf("%s served despite absent allowlist", name)
		}
	}
}
