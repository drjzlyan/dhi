package toolchain

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rustArchive mimics Rust's standalone installer: one directory per
// component, listed in a components file.
var rustArchive = buildTarGz([]tarEntry{
	{name: "rust-1.0.0-x/components", content: "rustc\ncargo\nrust-std-x\nrust-docs\n"},
	{name: "rust-1.0.0-x/rustc/bin/rustc", content: "RUSTC", exec: true},
	{name: "rust-1.0.0-x/rustc/manifest.in", content: "file:bin/rustc"},
	{name: "rust-1.0.0-x/cargo/bin/cargo", content: "CARGO", exec: true},
	{name: "rust-1.0.0-x/rust-std-x/lib/rustlib/x/lib/libstd.rlib", content: "STD"},
	{name: "rust-1.0.0-x/rust-docs/share/doc/index.html", content: "DOCS"},
})

func optionalManifest() *Manifest {
	sum := sha512.Sum512(rustArchive)
	mf := fixtureManifest()
	mf.Tools["rust"] = Tool{
		Version:  "1.0.0",
		Optional: true,
		Shims:    []string{"rustc", "cargo"},
		Platforms: map[string]PlatformSpec{PlatformKey(): {
			URL: "https://fixtures/rust.tar.gz", SHA512: hex.EncodeToString(sum[:]),
			Format: FormatTarGz, Strip: 1, BinDir: "bin", Merge: MergeRustComponents,
			Env: map[string]string{"RUST_SYSROOT": "{home}"},
		}},
	}
	return mf
}

func TestOptionalToolsInstallOnlyOnRequest(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	mf := optionalManifest()
	if err := mf.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(mf.Required(), ","); got != "node,rg" {
		t.Fatalf("required = %s", got)
	}
	if _, total := mf.DownloadSize(nil); total != 0 {
		t.Fatalf("first-run total counts optional tools: %d", total)
	}
	f := newFixtureServer(t, mf, map[string][]byte{
		"/rg.tar.gz": rgArchive, "/node.zip": nodeArchive, "/rust.tar.gz": rustArchive,
	}, false)
	ctx := context.Background()
	if err := m.Install(ctx, f.srv.URL+"/manifest.json", nil); err != nil {
		t.Fatal(err)
	}
	if m.Installed("rust") {
		t.Fatal("bootstrap installed an optional tool")
	}
	if err := m.Install(ctx, f.srv.URL+"/manifest.json", []string{"rust"}); err != nil {
		t.Fatal(err)
	}
	if !m.Installed("rust") {
		t.Fatal("rust not locked")
	}
	cur := filepath.Join(root, "tools", "rust", "current")
	for rel, want := range map[string]string{
		"bin/rustc": "RUSTC", "bin/cargo": "CARGO", "lib/rustlib/x/lib/libstd.rlib": "STD",
	} {
		if b, err := os.ReadFile(filepath.Join(cur, rel)); err != nil || string(b) != want {
			t.Fatalf("%s = %q, %v (components not merged into one sysroot)", rel, b, err)
		}
	}
	for _, gone := range []string{"rustc", "share/doc", "manifest.in", "rust-docs"} {
		if _, err := os.Stat(filepath.Join(cur, gone)); err == nil {
			t.Fatalf("%s left behind after the merge", gone)
		}
	}
	lf, _ := m.ReadLockfile()
	if !strings.HasPrefix(lf.Tools["rust"].SHA256, "sha512:") {
		t.Fatalf("locked digest = %q", lf.Tools["rust"].SHA256)
	}
}

func TestSHA512MismatchRefuses(t *testing.T) {
	m := New(t.TempDir())
	mf := optionalManifest()
	f := newFixtureServer(t, mf, map[string][]byte{"/rust.tar.gz": rustArchive}, true)
	err := m.Install(context.Background(), f.srv.URL+"/manifest.json", []string{"rust"})
	if err == nil || !strings.Contains(err.Error(), "sha512 mismatch") {
		t.Fatalf("tampered artifact: %v", err)
	}
}

func TestSpecDigestValidation(t *testing.T) {
	bad := []PlatformSpec{
		{URL: "https://x/a", SHA256: strings.Repeat("a", 64), SHA512: strings.Repeat("a", 128), Format: FormatTarGz},
		{URL: "https://x/a", SHA512: "abc", Format: FormatTarGz},
		{URL: "https://x/a", SHA256: strings.Repeat("a", 64), Format: FormatTarGz, Merge: "weird"},
	}
	for i, s := range bad {
		if err := validSpec("t", "p", s); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}
