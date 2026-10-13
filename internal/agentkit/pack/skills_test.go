package pack

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// skillPack writes a pack shipping one skill with a script beside it.
func skillPack(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "skills"), 0o755)
	os.WriteFile(filepath.Join(root, "skills", "lint.md"),
		[]byte("---\nname: Lint\ndescription: run the linter\nscript: lint.sh\n---\n\nRun the linter.\n"), 0o644)
	os.WriteFile(filepath.Join(root, "skills", "lint.sh"), []byte("#!/bin/sh\necho lint\n"), 0o644)
	os.WriteFile(filepath.Join(root, "pack.toml"),
		[]byte("schema = 2\nname = \"lint\"\nversion = \"1\"\nskills = [\"skills/lint.md\"]\n"), 0o644)
	return root
}

// F-091: a pack's skill script is installed beside its doc, executable,
// and removed again on uninstall (it used to be dropped).
func TestInstallCopiesSkillScriptsAndUninstallRemovesThem(t *testing.T) {
	ws := setupWS(t)
	in := &Installer{WS: ws}
	if _, err := in.Install(context.Background(), skillPack(t)); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(ws.Root, workspace.DirSkills, "lint.sh")
	fi, err := os.Stat(script)
	if err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("script %v err=%v", fi, err)
	}
	if err := in.Uninstall("lint"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatal("script left behind")
	}
}

func TestReadSkillsRefusesAMissingScript(t *testing.T) {
	root := skillPack(t)
	os.Remove(filepath.Join(root, "skills", "lint.sh"))
	if _, _, err := ReadSkills(root); err == nil {
		t.Fatal("missing script accepted")
	}
}
