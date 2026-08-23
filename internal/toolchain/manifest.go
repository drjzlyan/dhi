// Package toolchain installs and manages DHI's hermetic dependencies
// (git, ripgrep, node, uv) inside an XDG-isolated prefix, driven by a
// checksum-pinned registry manifest (ADR-0005). Downloads are verified
// before extraction, activation is atomic, and state is recorded in a
// lockfile. Shims are exposed only to DHI's own child processes.
package toolchain

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strings"
)

// SchemaVersion is the registry manifest schema this build understands.
// Manifests declaring any other schema are rejected.
const SchemaVersion = 1

var (
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	shimNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Format is an archive container the extractor supports.
type Format string

// Supported archive formats.
const (
	FormatTarGz Format = "tar.gz"
	FormatZip   Format = "zip"
)

// Valid reports whether f is a supported format.
func (f Format) Valid() bool {
	return f == FormatTarGz || f == FormatZip
}

// Manifest is the parsed registry manifest: pinned versions, URLs, and
// checksums for every tool DHI manages, per platform. It is a
// security-sensitive supply-chain input; treat edits like code review.
type Manifest struct {
	Schema int             `json:"schema"`
	Tools  map[string]Tool `json:"tools"`
}

// Tool pins one managed tool.
type Tool struct {
	Version   string                  `json:"version"`
	Platforms map[string]PlatformSpec `json:"platforms"`
	// Shims are the executable names placed on DHI's child-process PATH.
	Shims []string `json:"shims"`
}

// PlatformSpec pins one downloadable artifact for a "os/arch" key.
type PlatformSpec struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Format Format `json:"format"`
	// Strip drops N leading path components from every archive entry
	// (e.g. 1 for a GitHub-style "name-version/" root). 0 keeps layout.
	Strip int `json:"strip,omitempty"`
	// BinDir is the directory relative to the extraction root that holds
	// executables; empty means the root itself.
	BinDir string `json:"bin_dir,omitempty"`
}

// PlatformKey is the manifest key for the running platform ("os/arch").
func PlatformKey() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// ParseManifest decodes and validates manifest JSON.
func ParseManifest(data []byte) (*Manifest, error) {
	var mf Manifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("toolchain: manifest: %w", err)
	}
	if err := mf.Validate(); err != nil {
		return nil, err
	}
	return &mf, nil
}

// Validate checks structural integrity and pin hygiene. Remote URLs must
// be https; plaintext http is accepted only for loopback hosts so tests
// can drive the pipeline against fixture servers.
func (mf *Manifest) Validate() error {
	if mf.Schema != SchemaVersion {
		return fmt.Errorf("toolchain: manifest schema %d, want %d", mf.Schema, SchemaVersion)
	}
	if len(mf.Tools) == 0 {
		return fmt.Errorf("toolchain: manifest lists no tools")
	}
	for name, tool := range mf.Tools {
		if err := validToolName(name); err != nil {
			return err
		}
		if tool.Version == "" {
			return fmt.Errorf("toolchain: %s: missing version", name)
		}
		if len(tool.Platforms) == 0 {
			return fmt.Errorf("toolchain: %s: no platform artifacts", name)
		}
		if len(tool.Shims) == 0 {
			return fmt.Errorf("toolchain: %s: no shims", name)
		}
		for _, shim := range tool.Shims {
			if !shimNameRe.MatchString(shim) {
				return fmt.Errorf("toolchain: %s: bad shim name %q", name, shim)
			}
		}
		for plat, spec := range tool.Platforms {
			if err := validSpec(name, plat, spec); err != nil {
				return err
			}
		}
	}
	return nil
}

// Spec returns the artifact pinned for name on the running platform.
func (mf *Manifest) Spec(name string) (PlatformSpec, error) {
	tool, ok := mf.Tools[name]
	if !ok {
		return PlatformSpec{}, fmt.Errorf("toolchain: unknown tool %q", name)
	}
	spec, ok := tool.Platforms[PlatformKey()]
	if !ok {
		return PlatformSpec{}, fmt.Errorf("toolchain: %s: no artifact for %s", name, PlatformKey())
	}
	return spec, nil
}

func validSpec(tool, plat string, spec PlatformSpec) error {
	u, err := url.Parse(spec.URL)
	if err != nil {
		return fmt.Errorf("toolchain: %s/%s: bad url: %v", tool, plat, err)
	}
	switch {
	case u.Scheme == "https":
	case isLoopback(u):
	default:
		return fmt.Errorf("toolchain: %s/%s: url must be https (got %q)", tool, plat, spec.URL)
	}
	if !sha256Re.MatchString(spec.SHA256) {
		return fmt.Errorf("toolchain: %s/%s: sha256 must be 64 hex chars", tool, plat)
	}
	if !spec.Format.Valid() {
		return fmt.Errorf("toolchain: %s/%s: unsupported format %q", tool, plat, spec.Format)
	}
	if spec.Strip < 0 {
		return fmt.Errorf("toolchain: %s/%s: negative strip", tool, plat)
	}
	return nil
}

func validToolName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return fmt.Errorf("toolchain: bad tool name %q", name)
	}
	return nil
}

func isLoopback(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
