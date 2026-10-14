package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-088: DHI's credentials and grants files are fenced like ~/.ssh.
func TestSeatbeltDeniesReadingDHISecrets(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	p, err := seatbeltProfile([]string{t.TempDir()}, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"credentials.toml", "grants.json"} {
		want := `(deny file-read* (subpath ` + sbplQuote(filepath.Join(cfg, "dhi", f)) + `))`
		if !strings.Contains(p, want) {
			t.Fatalf("profile lacks %s", want)
		}
	}
}

// A bound root that covers the config dir gets the secrets file masked.
func TestBubblewrapMasksSecretsInsideABoundRoot(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	cred := filepath.Join(cfg, "dhi", "credentials.toml")
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &Bubblewrap{bin: "bwrap", rw: []string{cfg}}
	argv, err := b.Wrap([]string{"cat", cred})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(argv, " "), "--ro-bind /dev/null "+cred) {
		t.Fatalf("secret not masked: %v", argv)
	}
	b = &Bubblewrap{bin: "bwrap", rw: []string{t.TempDir()}}
	argv, _ = b.Wrap([]string{"true"})
	if strings.Contains(strings.Join(argv, " "), "/dev/null") {
		t.Fatalf("masked a path outside every root: %v", argv)
	}
}

// F-086 (live): shells and git write /dev/null; without it every
// redirect fails EPERM inside the sandbox.
func TestSeatbeltAllowsDeviceNodeWrites(t *testing.T) {
	p, err := seatbeltProfile([]string{t.TempDir()}, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, `(allow file-write* (literal "/dev/null")`) {
		t.Fatalf("profile lacks /dev/null:\n%s", p)
	}
}

// Live (M32): codex reads managed preferences through cfprefsd's shared
// memory and will not start without read access to it.
func TestSeatbeltAllowsPreferenceReadsOnly(t *testing.T) {
	p, err := seatbeltProfile([]string{t.TempDir()}, nil, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "(allow ipc-posix-shm-read*)") || !strings.Contains(p, "(allow user-preference-read)") {
		t.Fatal("preference reads not allowed")
	}
	if strings.Contains(p, "user-preference-write") || strings.Contains(p, "(allow ipc-posix-shm*)") {
		t.Fatal("more than reads allowed")
	}
}

func TestSeatbeltExtraReadOnlyIsExecutableNotWritable(t *testing.T) {
	sb, err := NewSeatbelt("/usr/bin/sandbox-exec", []string{t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ext, err := sb.WithExtraReadOnly([]string{"/opt/tools"})
	if err != nil {
		t.Fatal(err)
	}
	p := ext.(*Seatbelt).Profile()
	var exec, write string
	for _, l := range strings.Split(p, "\n") {
		switch {
		case strings.HasPrefix(l, "(allow process-exec*"):
			exec = l
		case strings.HasPrefix(l, "(allow file-write*") && strings.Contains(l, "(subpath"):
			write = l
		}
	}
	if !strings.Contains(exec, `"/opt/tools"`) || strings.Contains(write, `"/opt/tools"`) {
		t.Fatalf("exec=%s\nwrite=%s", exec, write)
	}
}
