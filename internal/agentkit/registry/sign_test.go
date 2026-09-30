package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func keypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// writeSignedIndex writes index.toml (+ optional .sig) into a temp dir.
func writeSignedIndex(t *testing.T, priv ed25519.PrivateKey, sign bool) string {
	t.Helper()
	dir := makeIndexDir(t, map[string]string{})
	if sign {
		data, err := os.ReadFile(filepath.Join(dir, "index.toml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sigFile), []byte(SignIndex(priv, data)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestVerifyIndexRoundTrip(t *testing.T) {
	pub, priv := keypair(t)
	data := []byte("schema = 1\n")
	sig := SignIndex(priv, data)
	if err := VerifyIndex(data, sig, []ed25519.PublicKey{pub}); err != nil {
		t.Fatalf("valid signature refused: %v", err)
	}
	// Wrong key refuses.
	other, _ := keypair(t)
	if err := VerifyIndex(data, sig, []ed25519.PublicKey{other}); err == nil {
		t.Fatal("signature verified against the wrong key")
	}
	// Tampered data refuses.
	if err := VerifyIndex([]byte("schema = 2\n"), sig, []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("tampered data verified")
	}
	// No keys / malformed signature / wrong length are named.
	if err := VerifyIndex(data, sig, nil); err == nil || !strings.Contains(err.Error(), "no trusted key") {
		t.Fatalf("empty keys = %v", err)
	}
	if err := VerifyIndex(data, "zz", []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("non-hex signature accepted")
	}
	if err := VerifyIndex(data, "abcd", []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("short signature accepted")
	}
}

func TestTrustKeyIdempotent(t *testing.T) {
	r := New(newWS(t))
	pub, _ := keypair(t)
	hexKey := PublicKeyHex(pub)
	if err := r.TrustKey(hexKey); err != nil {
		t.Fatal(err)
	}
	if err := r.TrustKey(hexKey); err != nil {
		t.Fatal(err)
	}
	keys, err := r.TrustedKeys()
	if err != nil || len(keys) != 1 || keys[0] != hexKey {
		t.Fatalf("keys = %v err=%v", keys, err)
	}
	if err := r.TrustKey("not-hex"); err == nil {
		t.Fatal("malformed key accepted")
	}
	if err := r.UntrustKey(hexKey); err != nil {
		t.Fatal(err)
	}
	if keys, _ := r.TrustedKeys(); len(keys) != 0 {
		t.Fatalf("keys after untrust = %v", keys)
	}
}

func TestRefreshSignaturePolicy(t *testing.T) {
	ctx := context.Background()
	pub, priv := keypair(t)
	otherPub, otherPriv := keypair(t)
	_ = otherPub

	// Digest-only mode: unsigned source, no pinned key → ok.
	r := New(newWS(t))
	if err := r.Refresh(ctx, writeSignedIndex(t, priv, false)); err != nil {
		t.Fatalf("unsigned refresh (no keys): %v", err)
	}
	if _, err := r.Browse(); err != nil {
		t.Fatalf("browse after digest-only refresh: %v", err)
	}

	// Pinned key + unsigned source → refuse by name.
	r2 := New(newWS(t))
	if err := r2.TrustKey(PublicKeyHex(pub)); err != nil {
		t.Fatal(err)
	}
	if err := r2.Refresh(ctx, writeSignedIndex(t, priv, false)); err == nil ||
		!strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("unsigned refresh with pinned key = %v", err)
	}

	// Pinned key + valid signature → ok, and cached reads verify.
	r3 := New(newWS(t))
	if err := r3.TrustKey(PublicKeyHex(pub)); err != nil {
		t.Fatal(err)
	}
	if err := r3.Refresh(ctx, writeSignedIndex(t, priv, true)); err != nil {
		t.Fatalf("signed refresh: %v", err)
	}
	if _, err := r3.Browse(); err != nil {
		t.Fatalf("browse after signed refresh: %v", err)
	}

	// Signature from an untrusted key → refuse, nothing cached.
	r4 := New(newWS(t))
	if err := r4.TrustKey(PublicKeyHex(pub)); err != nil {
		t.Fatal(err)
	}
	if err := r4.Refresh(ctx, writeSignedIndex(t, otherPriv, true)); err == nil ||
		!strings.Contains(err.Error(), "trusted key") {
		t.Fatalf("untrusted signer = %v", err)
	}

	// A tampered cache refuses at read time (sig stored at refresh).
	if err := os.WriteFile(r3.cachePath(), []byte("schema = 1\n# tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r3.Browse(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered cache = %v", err)
	}
}

func TestCachedIndexWithoutSignatureRefuses(t *testing.T) {
	r := New(newWS(t))
	pub, _ := keypair(t)
	// Cache written but no .sig (a legacy/unsigned cache).
	if err := os.MkdirAll(r.dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(r.cachePath(), []byte("schema = 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := r.TrustKey(PublicKeyHex(pub)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Browse(); err == nil || !strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("unsigned cache with pinned key = %v", err)
	}
}
