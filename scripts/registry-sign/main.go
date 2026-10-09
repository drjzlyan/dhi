// Command registry-sign is the publisher side of pack-index signatures
// (F-034 follow-on, ADR-0022; F-062): it makes an Ed25519 key pair and
// signs a registry's index.toml, so users who pin the public key
// (Settings › MARKETPLACE › publisher key) refuse any index that does
// not verify.
//
//	go run ./scripts/registry-sign keygen <dir>          # writes <dir>/publisher.key, prints the public key
//	go run ./scripts/registry-sign sign <key> <index>    # writes <index>.sig beside index.toml
//	go run ./scripts/registry-sign verify <pub> <index>  # checks a signed index with a public key
//
// Keep publisher.key secret (it is written 0600); publish only the
// public key, e.g. in the registry's README.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/registry"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "registry-sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: keygen <dir> | sign <publisher.key> <index.toml> | verify <public-key-hex> <index.toml>")
	}
	switch args[0] {
	case "keygen":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		path := filepath.Join(args[1], "publisher.key")
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s exists; refusing to overwrite a key", path)
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(priv)+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Printf("private key: %s (keep it secret)\npublic key:  %s\n", path, hex.EncodeToString(pub))
		return nil
	case "sign":
		if len(args) != 3 {
			return fmt.Errorf("usage: sign <publisher.key> <index.toml>")
		}
		raw, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		priv, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(priv) != ed25519.PrivateKeySize {
			return fmt.Errorf("%s is not an Ed25519 private key in hex", args[1])
		}
		index, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		sig := registry.SignIndex(ed25519.PrivateKey(priv), index)
		out := args[2] + ".sig"
		if err := os.WriteFile(out, []byte(sig+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", out)
		return nil
	case "verify":
		if len(args) != 3 {
			return fmt.Errorf("usage: verify <public-key-hex> <index.toml>")
		}
		pub, err := hex.DecodeString(strings.TrimSpace(args[1]))
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return fmt.Errorf("not an Ed25519 public key in hex")
		}
		index, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(args[2] + ".sig")
		if err != nil {
			return err
		}
		if err := registry.VerifyIndex(index, strings.TrimSpace(string(sig)), []ed25519.PublicKey{pub}); err != nil {
			return err
		}
		fmt.Println("signature OK")
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}
