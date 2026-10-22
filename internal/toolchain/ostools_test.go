package toolchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyToClipboardRunsTheOSTool(t *testing.T) {
	out := filepath.Join(t.TempDir(), "clip")
	clipboardArgv = func() []string { return []string{"/bin/sh", "-c", "cat > " + out} }
	t.Cleanup(func() { clipboardArgv = defaultClipboardArgv })
	if err := CopyToClipboard("héllo"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "héllo" {
		t.Fatalf("clipboard got %q", b)
	}
	clipboardArgv = func() []string { return nil }
	if err := CopyToClipboard("x"); err != ErrNoOSTool {
		t.Fatalf("no tool: %v", err)
	}
}

func TestOpenURLRefusesNonWebURLs(t *testing.T) {
	called := false
	openArgv = func(string) []string { called = true; return []string{"/usr/bin/true"} }
	t.Cleanup(func() { openArgv = defaultOpenArgv })
	if err := OpenURL("file:///etc/passwd"); err == nil || called {
		t.Fatal("a file URL must never reach the opener")
	}
	if err := OpenURL("https://example.com/login"); err != nil || !called {
		t.Fatalf("https: err=%v called=%v", err, called)
	}
}
