package toolchain

import (
	"strings"
	"testing"
)

func validManifestJSON() []byte {
	return []byte(`{
  "schema": 1,
  "tools": {
    "rg": {
      "version": "14.1.0",
      "platforms": {
        "` + PlatformKey() + `": {
          "url": "https://example.test/rg.tar.gz",
          "sha256": "` + strings.Repeat("ab", 32) + `",
          "format": "tar.gz",
          "strip": 1
        }
      },
      "shims": ["rg"]
    }
  }
}`)
}

func TestParseManifestValid(t *testing.T) {
	mf, err := ParseManifest(validManifestJSON())
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	tool, ok := mf.Tools["rg"]
	if !ok {
		t.Fatal("tool rg missing")
	}
	if tool.Version != "14.1.0" {
		t.Errorf("version = %q", tool.Version)
	}
	spec, err := mf.Spec("rg")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if spec.Strip != 1 || spec.Format != FormatTarGz {
		t.Errorf("spec = %+v", spec)
	}
}

func TestManifestValidateErrors(t *testing.T) {
	baseSpec := func() PlatformSpec {
		return PlatformSpec{
			URL:    "https://x.test/a.tgz",
			SHA256: strings.Repeat("0", 64),
			Format: FormatTarGz,
		}
	}
	baseTool := func() Tool {
		return Tool{
			Version:   "1.0",
			Platforms: map[string]PlatformSpec{PlatformKey(): baseSpec()},
			Shims:     []string{"rg"},
		}
	}

	tests := map[string]*Manifest{
		"wrong schema": {
			Schema: 99,
			Tools:  map[string]Tool{"rg": baseTool()},
		},
		"empty version": {Schema: SchemaVersion, Tools: map[string]Tool{"rg": {Platforms: baseTool().Platforms, Shims: []string{"rg"}}}},
		"no platforms":  {Schema: SchemaVersion, Tools: map[string]Tool{"rg": {Version: "1.0", Shims: []string{"rg"}}}},
		"no shims":      {Schema: SchemaVersion, Tools: map[string]Tool{"rg": {Version: "1.0", Platforms: baseTool().Platforms}}},
		"bad shim": {Schema: SchemaVersion, Tools: map[string]Tool{
			"rg": {Version: "1.0", Platforms: baseTool().Platforms, Shims: []string{"../escape"}},
		}},
		"short digest": {Schema: SchemaVersion, Tools: map[string]Tool{
			"rg": {Version: "1.0",
				Platforms: map[string]PlatformSpec{PlatformKey(): {URL: "https://x.test/a.tgz", SHA256: "abc123", Format: FormatTarGz}},
				Shims:     []string{"rg"}},
		}},
		"ftp url": {Schema: SchemaVersion, Tools: map[string]Tool{
			"rg": {Version: "1.0",
				Platforms: map[string]PlatformSpec{PlatformKey(): {URL: "ftp://x.test/a", SHA256: strings.Repeat("0", 64), Format: FormatTarGz}},
				Shims:     []string{"rg"}},
		}},
		"remote http": {Schema: SchemaVersion, Tools: map[string]Tool{
			"rg": {Version: "1.0",
				Platforms: map[string]PlatformSpec{PlatformKey(): {URL: "http://evil.test/a", SHA256: strings.Repeat("0", 64), Format: FormatTarGz}},
				Shims:     []string{"rg"}},
		}},
		"bad format": {Schema: SchemaVersion, Tools: map[string]Tool{
			"rg": {Version: "1.0",
				Platforms: map[string]PlatformSpec{PlatformKey(): {URL: "https://x.test/a.rar", SHA256: strings.Repeat("0", 64), Format: "rar"}},
				Shims:     []string{"rg"}},
		}},
		"negative strip": {Schema: SchemaVersion, Tools: map[string]Tool{
			"rg": func() Tool {
				tl := baseTool()
				sp := baseSpec()
				sp.Strip = -1
				tl.Platforms = map[string]PlatformSpec{PlatformKey(): sp}
				return tl
			}(),
		}},
		"bad tool name": {Schema: SchemaVersion, Tools: map[string]Tool{"../rg": baseTool()}},
	}
	for label, mf := range tests {
		if err := mf.Validate(); err == nil {
			t.Errorf("%s: Validate passed, want error", label)
		}
	}
}

func TestEmptyManifestIsValid(t *testing.T) {
	mf, err := ParseManifest([]byte(`{"schema":1,"tools":{}}`))
	if err != nil {
		t.Fatalf("seed manifest rejected: %v", err)
	}
	if len(mf.Tools) != 0 {
		t.Errorf("tools = %v, want empty", mf.Tools)
	}
}

// TestEmbeddedRegistryValid guards the in-binary supply-chain anchor:
// it must always parse and validate, even while unpinned.
func TestEmbeddedRegistryValid(t *testing.T) {
	mf, err := Embedded()
	if err != nil {
		t.Fatalf("embedded registry invalid: %v", err)
	}
	if mf.Schema != SchemaVersion {
		t.Errorf("embedded schema = %d", mf.Schema)
	}
	for name, tool := range mf.Tools {
		if _, err := mf.Spec(name); err != nil {
			t.Errorf("tool %s has no artifact for this platform: %v", name, err)
		}
		if tool.Version == "" || len(tool.Shims) == 0 {
			t.Errorf("tool %s incompletely pinned: %+v", name, tool)
		}
	}
}

func TestManifestLoopbackHTTPTAllowed(t *testing.T) {
	mf := &Manifest{
		Schema: SchemaVersion,
		Tools: map[string]Tool{
			"rg": {
				Version: "1.0",
				Platforms: map[string]PlatformSpec{
					PlatformKey(): {URL: "http://127.0.0.1:1234/rg.tgz", SHA256: strings.Repeat("0", 64), Format: FormatZip},
				},
				Shims: []string{"rg"},
			},
		},
	}
	if err := mf.Validate(); err != nil {
		t.Fatalf("loopback http rejected: %v", err)
	}
}

func TestSpecMissingPlatform(t *testing.T) {
	mf := &Manifest{
		Schema: SchemaVersion,
		Tools: map[string]Tool{
			"rg": {
				Version:   "1.0",
				Platforms: map[string]PlatformSpec{"plan9/amd64": {}},
				Shims:     []string{"rg"},
			},
		},
	}
	if _, err := mf.Spec("rg"); err == nil {
		t.Fatal("Spec passed for absent platform, want error")
	}
	if _, err := mf.Spec("nope"); err == nil {
		t.Fatal("Spec passed for unknown tool, want error")
	}
}
