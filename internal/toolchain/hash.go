package toolchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HashFile returns the lowercase sha256 hex digest of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("toolchain: hash %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("toolchain: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyHash returns nil when the file's sha256 digest equals wantHex
// (case-insensitive). Verification always happens before extraction;
// unverified bytes are never unpacked (ADR-0005).
func VerifyHash(path, wantHex string) error {
	got, err := HashFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("toolchain: sha256 mismatch for %s: got %s want %s",
			filepath.Base(path), got, wantHex)
	}
	return nil
}
