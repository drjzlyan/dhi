// Index signature verification (F-034 Part B follow-on, ADR-0022): the
// index's digest pins are only as trustworthy as the index itself, so a
// publisher may sign index.toml with an Ed25519 key. The user pins the
// publisher's public key in .dhi/registry/trusted_keys; refresh then
// requires a detached index.toml.sig that verifies against a trusted
// key before the index is cached. With no trusted key the registry stays
// in its documented digest-only mode (the pin IS the trust anchor).
package registry

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sigFile is the detached signature the publisher ships beside
// index.toml (lowercase hex of the 64-byte signature).
const sigFile = "index.toml.sig"

// ParsePublicKey decodes a 32-byte Ed25519 public key from lowercase hex.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("registry: public key: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("registry: public key must be %d hex-decoded bytes, got %d", ed25519.PublicKeySize, len(b))
	}
	return ed25519.PublicKey(b), nil
}

// PublicKeyHex renders a public key as lowercase hex (the trusted-keys
// file format).
func PublicKeyHex(pub ed25519.PublicKey) string { return hex.EncodeToString(pub) }

// VerifyIndex checks sigHex (lowercase hex of a detached Ed25519
// signature) over data against any trusted key. An empty key set is a
// named error: callers enforce the mode where keys exist.
func VerifyIndex(data []byte, sigHex string, keys []ed25519.PublicKey) error {
	if len(keys) == 0 {
		return fmt.Errorf("registry: no trusted key configured")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil {
		return fmt.Errorf("registry: signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("registry: signature must be %d hex-decoded bytes, got %d", ed25519.SignatureSize, len(sig))
	}
	for _, k := range keys {
		if ed25519.Verify(k, data, sig) {
			return nil
		}
	}
	return fmt.Errorf("registry: index signature does not match any trusted key")
}

// SignIndex produces the detached signature a publisher ships
// (lowercase hex). It exists for tooling and tests.
func SignIndex(priv ed25519.PrivateKey, data []byte) string {
	return hex.EncodeToString(ed25519.Sign(priv, data))
}

func (r *Registry) trustedKeyPath() string { return filepath.Join(r.dir(), "trusted_keys") }

// TrustedKeys returns the pinned publisher keys (hex), sorted.
func (r *Registry) TrustedKeys() ([]string, error) {
	data, err := os.ReadFile(r.trustedKeyPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registry: read trusted keys: %w", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}

// decodedKeys parses the pinned keys, refusing a malformed entry by name
// (a bad pin silently ignored would weaken every later verify).
func (r *Registry) decodedKeys() ([]ed25519.PublicKey, error) {
	hexes, err := r.TrustedKeys()
	if err != nil {
		return nil, err
	}
	var out []ed25519.PublicKey
	for _, h := range hexes {
		k, err := ParsePublicKey(h)
		if err != nil {
			return nil, fmt.Errorf("registry: trusted key %q: %w", h, err)
		}
		out = append(out, k)
	}
	return out, nil
}

// TrustKey pins a publisher public key (idempotent); a malformed key is
// refused before anything is written.
func (r *Registry) TrustKey(hexKey string) error {
	pub, err := ParsePublicKey(hexKey)
	if err != nil {
		return err
	}
	norm := PublicKeyHex(pub)
	for _, k := range mustKeys(r) {
		if k == norm {
			return nil
		}
	}
	if err := os.MkdirAll(r.dir(), 0o755); err != nil {
		return fmt.Errorf("registry: mkdir: %w", err)
	}
	keys, _ := r.TrustedKeys()
	keys = append(keys, norm)
	sort.Strings(keys)
	return writeAtomic(r.trustedKeyPath(), []byte(strings.Join(keys, "\n")+"\n"))
}

// UntrustKey removes a pinned key; missing is a no-op.
func (r *Registry) UntrustKey(hexKey string) error {
	pub, err := ParsePublicKey(hexKey)
	if err != nil {
		return err
	}
	drop := PublicKeyHex(pub)
	keys, err := r.TrustedKeys()
	if err != nil {
		return err
	}
	var kept []string
	for _, k := range keys {
		if k != drop {
			kept = append(kept, k)
		}
	}
	if len(kept) == 0 {
		if err := os.Remove(r.trustedKeyPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeAtomic(r.trustedKeyPath(), []byte(strings.Join(kept, "\n")+"\n"))
}

func mustKeys(r *Registry) []string {
	keys, _ := r.TrustedKeys()
	return keys
}
