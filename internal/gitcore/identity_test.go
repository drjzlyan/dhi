package gitcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGitStub(t *testing.T, script string) *Runner {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "git")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewRunner(bin, nil)
}

func TestResolveIdentity(t *testing.T) {
	r := writeGitStub(t,
		"#!/bin/sh\ncase \"$3\" in\nuser.name) echo 'Ada Lovelace';;\nuser.email) echo 'ada@example.com';;\n*) exit 1;;\nesac\n")
	id, err := ResolveIdentity(context.Background(), r)
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	if id.Name != "Ada Lovelace" || id.Email != "ada@example.com" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestResolveIdentityUnsetRefuses(t *testing.T) {
	r := writeGitStub(t, "#!/bin/sh\nexit 1\n")
	_, err := ResolveIdentity(context.Background(), r)
	if !errors.Is(err, ErrIdentityUnset) {
		t.Fatalf("err = %v, want ErrIdentityUnset", err)
	}
	if !strings.Contains(err.Error(), "git config --global user.name") {
		t.Fatalf("refusal must name the fix: %v", err)
	}
}

func TestSetIdentityWritesBothKeysGlobally(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	r := writeGitStub(t, "#!/bin/sh\necho \"$@\" >> "+log+"\n")
	err := SetIdentity(context.Background(), r, Identity{Name: " Ada Lovelace ", Email: "ada@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(log)
	want := "config --global user.name Ada Lovelace\nconfig --global user.email ada@example.com\n"
	if string(got) != want {
		t.Fatalf("calls =\n%q\nwant\n%q", got, want)
	}
}

func TestSetIdentityValidates(t *testing.T) {
	r := writeGitStub(t, "#!/bin/sh\nexit 0\n")
	for _, id := range []Identity{
		{"", "a@b.c"}, {"Ada", ""}, {"Ada", "nope"}, {"Ada", "a b@c.d"}, {"Ad\na", "a@b.c"},
	} {
		if err := SetIdentity(context.Background(), r, id); err == nil {
			t.Errorf("accepted %+v", id)
		}
	}
}

func TestIdentityCommandsQuote(t *testing.T) {
	cmds := IdentityCommands(Identity{Name: "O'Neil", Email: "o@x.io"})
	if cmds[0] != `git config --global user.name 'O'\''Neil'` || cmds[1] != "git config --global user.email 'o@x.io'" {
		t.Fatalf("cmds = %q", cmds)
	}
}
