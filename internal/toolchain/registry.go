package toolchain

import (
	_ "embed"
)

// The production registry manifest is embedded in every DHI binary
// (user decision 2026-08-23): pins ship with releases, so the trust
// anchor is the binary itself. registry/manifest.json is
// security-sensitive supply chain — changes are reviewed like code and
// every URL is pinned to an exact sha256.
//
// The seed manifest lists no tools: bootstrap then resolves to zero
// actions and doctor reports what is missing (ADR-0005 degrade-visibly).
// It is populated by the pinning workflow once artifact URLs + digests
// are finalized per platform.

//go:embed registry/manifest.json
var registryJSON []byte

// Embedded parses the in-binary registry manifest.
func Embedded() (*Manifest, error) {
	return ParseManifest(registryJSON)
}
