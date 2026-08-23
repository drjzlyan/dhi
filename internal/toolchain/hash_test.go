package toolchain

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashFileAndVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	content := []byte("dhi fixture payload\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])

	if got, err := HashFile(path); err != nil || got != want {
		t.Fatalf("HashFile = %q, %v; want %q", got, err, want)
	}
	if err := VerifyHash(path, want); err != nil {
		t.Fatalf("VerifyHash correct digest: %v", err)
	}
	if err := VerifyHash(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("VerifyHash wrong digest passed")
	} else if !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("error = %v, want mismatch message", err)
	}
}

func TestVerifyHashMissingFile(t *testing.T) {
	if err := VerifyHash(filepath.Join(t.TempDir(), "absent"), strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected error for missing file")
	}
}
