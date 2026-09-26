package dhitools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// callDenied runs a mutating tool and answers the parked approval with a
// denial, returning the tool's agent-visible outcome.
func callDenied(h mcp.Handler, t *testing.T, name, argsJSON string, f *fixture) (string, bool) {
	t.Helper()
	type result struct {
		out   string
		isErr bool
	}
	done := make(chan result, 1)
	go func() {
		out, isErr, _ := h.CallTool(context.Background(), name, json.RawMessage(argsJSON))
		done <- result{out, isErr}
	}()
	deadline := 50
	for len(f.approvals.List()) == 0 && deadline > 0 {
		deadline--
		sleepTick()
	}
	if aps := f.approvals.List(); len(aps) > 0 {
		f.approvals.Resolve(aps[0].ID, false)
	}
	r := <-done
	return r.out, r.isErr
}

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

func TestFSWriteCreatesFile(t *testing.T) {
	f, m := newFixture(t, "write")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "write", `{"path":"api/new.go","content":"package api\n"}`, f)
	if out, isErr := resolve(t); isErr {
		t.Fatalf("write refused: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(memberDir(t, f.ws, "api"), "new.go"))
	if err != nil || string(got) != "package api\n" {
		t.Fatalf("file = %q err = %v", got, err)
	}
}

func TestFSWriteOverwritesFile(t *testing.T) {
	f, m := newFixture(t, "write")
	writeFile(t, memberDir(t, f.ws, "api"), "a.go", "old\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "write", `{"path":"api/a.go","content":"new\n"}`, f)
	if out, isErr := resolve(t); isErr {
		t.Fatalf("write refused: %s", out)
	}
	got, _ := os.ReadFile(filepath.Join(memberDir(t, f.ws, "api"), "a.go"))
	if string(got) != "new\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestFSWriteDeniedLeavesNoFile(t *testing.T) {
	f, m := newFixture(t, "write")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	out, isErr := callDenied(h, t, "write", `{"path":"api/denied.go","content":"x\n"}`, f)
	if !isErr {
		t.Fatalf("denied write must refuse, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(memberDir(t, f.ws, "api"), "denied.go")); !os.IsNotExist(err) {
		t.Fatalf("denied write created a file: %v", err)
	}
}

func TestFSWriteRefusesEscapeBeforeApproval(t *testing.T) {
	f, m := newFixture(t, "write")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	// A malformed path is a schema refusal: no approval is parked.
	out, isErr := call(h, t, "write", `{"path":"api/../x","content":"y"}`)
	if !isErr || !strings.Contains(out, "escape") {
		t.Fatalf("escape = %q isErr=%v", out, isErr)
	}
	if len(f.approvals.List()) != 0 {
		t.Fatal("malformed path parked an approval")
	}
}

func TestFSWriteRefusesDirectory(t *testing.T) {
	f, m := newFixture(t, "write")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "write", `{"path":"api","content":"y"}`, f)
	if out, isErr := resolve(t); !isErr || !strings.Contains(out, "directory") {
		t.Fatalf("dir write = %q isErr=%v", out, isErr)
	}
}

func TestFSPatchReplacesUnique(t *testing.T) {
	f, m := newFixture(t, "patch")
	writeFile(t, memberDir(t, f.ws, "api"), "a.go", "foo bar baz\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "patch", `{"path":"api/a.go","old":"bar","new":"qux"}`, f)
	if out, isErr := resolve(t); isErr {
		t.Fatalf("patch refused: %s", out)
	}
	got, _ := os.ReadFile(filepath.Join(memberDir(t, f.ws, "api"), "a.go"))
	if string(got) != "foo qux baz\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestFSPatchRefusesNotFound(t *testing.T) {
	f, m := newFixture(t, "patch")
	writeFile(t, memberDir(t, f.ws, "api"), "a.go", "alpha\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "patch", `{"path":"api/a.go","old":"missing","new":"x"}`, f)
	if out, isErr := resolve(t); !isErr || !strings.Contains(out, "not found") {
		t.Fatalf("patch = %q isErr=%v, want not-found refusal", out, isErr)
	}
}

func TestFSPatchRefusesAmbiguousUnlessReplaceAll(t *testing.T) {
	f, m := newFixture(t, "patch")
	writeFile(t, memberDir(t, f.ws, "api"), "a.go", "x x x\n")
	h := Deps{Agent: m, WS: f.ws, Approvals: f.approvals}.Handler()

	resolve := callAsync(h, "patch", `{"path":"api/a.go","old":"x","new":"y"}`, f)
	if out, isErr := resolve(t); !isErr || !strings.Contains(out, "3") {
		t.Fatalf("ambiguous = %q isErr=%v, want a count-naming refusal", out, isErr)
	}

	resolve2 := callAsync(h, "patch", `{"path":"api/a.go","old":"x","new":"y","replace_all":true}`, f)
	if out, isErr := resolve2(t); isErr {
		t.Fatalf("replace_all refused: %s", out)
	}
	got, _ := os.ReadFile(filepath.Join(memberDir(t, f.ws, "api"), "a.go"))
	if string(got) != "y y y\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestFSWritePatchServed(t *testing.T) {
	for _, s := range []string{"write", "patch"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}
