package toolchain

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readReleasePlatforms parses scripts/release-platforms.txt.
func readReleasePlatforms(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "scripts", "release-platforms.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// A shipped binary whose first run cannot install its toolchain is worse
// than no binary: boot refuses (ADR-0011). Every release platform must
// therefore be pinned for every managed tool.
func TestEveryReleasePlatformIsPinnedForEveryTool(t *testing.T) {
	mf, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	platforms := readReleasePlatforms(t)
	if len(platforms) == 0 {
		t.Fatal("scripts/release-platforms.txt lists no platforms")
	}
	for _, plat := range platforms {
		for name, tool := range mf.Tools {
			spec, ok := tool.Platforms[plat]
			if !ok {
				t.Errorf("release platform %s has no %s artifact in the manifest — pin it (and extend the hermetic-git matrix) before shipping it", plat, name)
				continue
			}
			if spec.Size <= 0 {
				t.Errorf("%s/%s: no download size recorded — run scripts/pin-sizes.py (the first-run screen shows it)", name, plat)
			}
			if spec.URL == "" || len(spec.SHA256) != 64 {
				t.Errorf("%s/%s: incomplete pin (url=%q sha256 len=%d)", name, plat, spec.URL, len(spec.SHA256))
			}
		}
	}
}

func TestDownloadSizeTotalsThisPlatform(t *testing.T) {
	mf := &Manifest{Tools: map[string]Tool{
		"a": {Platforms: map[string]PlatformSpec{PlatformKey(): {Size: 100}}},
		"b": {Platforms: map[string]PlatformSpec{PlatformKey(): {Size: 50}}},
		"c": {Platforms: map[string]PlatformSpec{"plan9/mips": {Size: 7}}},
	}}
	per, total := mf.DownloadSize(nil)
	if total != 150 || per["a"] != 100 || per["b"] != 50 {
		t.Fatalf("per=%v total=%d", per, total)
	}
	if _, tot := mf.DownloadSize([]string{"b"}); tot != 50 {
		t.Fatalf("subset total = %d", tot)
	}
}
