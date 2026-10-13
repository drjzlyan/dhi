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
	"sort"
	"strings"
)

// SchemaVersion is the registry manifest schema this build understands.
// Manifests declaring any other schema are rejected.
const SchemaVersion = 1

var (
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sha512Re   = regexp.MustCompile(`^[0-9a-f]{128}$`)
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
	// Optional tools (F-076: rust, jdk, maven, gradle, dotnet) are not part
	// of the first-run install; DHI installs one when a project first
	// needs it, after the human approves the download.
	Optional bool `json:"optional,omitempty"`
}

// PlatformSpec pins one downloadable artifact for a "os/arch" key.
type PlatformSpec struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
	// SHA512 pins vendors that publish only a sha512 (F-076); exactly one
	// of SHA256 and SHA512 is set.
	SHA512 string `json:"sha512,omitempty"`
	// Merge folds a multi-component archive into one tree after
	// extraction: "rust-components" merges the directories the archive's
	// `components` file lists (docs skipped) into a single sysroot.
	Merge string `json:"merge,omitempty"`
	// Env is set for DHI's builds and agents while the tool is managed:
	// "{home}" expands to the tool's stable current/ directory
	// (JAVA_HOME, DOTNET_ROOT).
	Env    map[string]string `json:"env,omitempty"`
	Format Format            `json:"format"`
	// Strip drops N leading path components from every archive entry
	// (e.g. 1 for a GitHub-style "name-version/" root). 0 keeps layout.
	Strip int `json:"strip,omitempty"`
	// BinDir is the directory relative to the extraction root that holds
	// executables; empty means the root itself.
	BinDir string `json:"bin_dir,omitempty"`
	// Size is the download size in bytes (scripts/pin-sizes.py), shown on
	// the first-run screen before anything is fetched. 0 = unknown.
	Size int64 `json:"size,omitempty"`
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
// can drive the pipeline against fixture servers. An empty tools map is
// legal (the embedded seed manifest) and resolves to zero actions.
func (mf *Manifest) Validate() error {
	if mf.Schema != SchemaVersion {
		return fmt.Errorf("toolchain: manifest schema %d, want %d", mf.Schema, SchemaVersion)
	}
	if mf.Tools == nil {
		mf.Tools = map[string]Tool{}
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
	switch {
	case spec.SHA256 != "" && spec.SHA512 != "":
		return fmt.Errorf("toolchain: %s/%s: pin sha256 or sha512, not both", tool, plat)
	case spec.SHA512 != "":
		if !sha512Re.MatchString(spec.SHA512) {
			return fmt.Errorf("toolchain: %s/%s: sha512 must be 128 hex chars", tool, plat)
		}
	case !sha256Re.MatchString(spec.SHA256):
		return fmt.Errorf("toolchain: %s/%s: sha256 must be 64 hex chars", tool, plat)
	}
	switch spec.Merge {
	case "", MergeRustComponents:
	default:
		return fmt.Errorf("toolchain: %s/%s: unknown merge %q", tool, plat, spec.Merge)
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

// DownloadSize reports the per-tool and total download bytes for names on
// this platform (all tools when names is empty). Tools without a pin for
// this platform or without a recorded size contribute 0.
func (mf *Manifest) DownloadSize(names []string) (per map[string]int64, total int64) {
	if len(names) == 0 {
		names = mf.Required()
	}
	per = map[string]int64{}
	for _, n := range names {
		spec, ok := mf.Tools[n].Platforms[PlatformKey()]
		if !ok {
			continue
		}
		per[n] = spec.Size
		total += spec.Size
	}
	return per, total
}

// MergeRustComponents is the PlatformSpec.Merge mode for Rust's
// standalone installer archives.
const MergeRustComponents = "rust-components"

// Required lists the tools the first-run install fetches: every
// non-optional tool, sorted.
func (mf *Manifest) Required() []string {
	var out []string
	for n, t := range mf.Tools {
		if !t.Optional {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Digest is the pinned digest, prefixed by algorithm for sha512.
func (s PlatformSpec) Digest() string {
	if s.SHA512 != "" {
		return "sha512:" + s.SHA512
	}
	return s.SHA256
}
