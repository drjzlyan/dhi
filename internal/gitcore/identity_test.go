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
