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
