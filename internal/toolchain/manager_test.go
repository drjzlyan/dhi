package toolchain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const fixtureDigestLen = 64

func marshalManifest(t *testing.T, mf *Manifest) []byte {
	t.Helper()
	data, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fixtureServer serves a manifest plus artifact bytes; artifactHits counts
// downloads to assert idempotence. tamper flips one byte of every served
// artifact so checksums no longer match the manifest.
type fixtureServer struct {
	srv          *httptest.Server
	artifactHits atomic.Int32
	tamper       bool
}

func newFixtureServer(t *testing.T, mf *Manifest, artifacts map[string][]byte, tamper bool) *fixtureServer {
	t.Helper()
	f := &fixtureServer{tamper: tamper}
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data := marshalManifest(t, mf)
		w.Write(bytes.ReplaceAll(data, []byte("https://fixtures"), []byte(f.srvURL())))
	})
	for path, data := range artifacts {
		served := data
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			f.artifactHits.Add(1)
			if f.tamper && len(served) > 0 {
				served = append([]byte(nil), served...)
				served[0] ^= 0xFF
			}
			w.Write(served)
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixtureServer) srvURL() string {
	if f.srv == nil {
		return "http://127.0.0.1:0" // manifest templating runs pre-listen
	}
	return f.srv.URL
}

func fixtureManifest() *Manifest {
	return &Manifest{
		Schema: SchemaVersion,
		Tools: map[string]Tool{
			"rg": {
				Version: "14.1.0",
				Platforms: map[string]PlatformSpec{
					PlatformKey(): {
						URL:    "https://fixtures/rg.tar.gz",
						SHA256: digestOf(rgArchive),
						Format: FormatTarGz,
						Strip:  1,
						BinDir: "bin",
					},
				},
				Shims: []string{"rg"},
			},
			"node": {
				Version: "22.3.0",
				Platforms: map[string]PlatformSpec{
					PlatformKey(): {
						URL:    "https://fixtures/node.zip",
						SHA256: digestOf(nodeArchive),
						Format: FormatZip,
						BinDir: "bin",
					},
				},
				Shims: []string{"node", "npm"},
			},
		},
	}
}

var (
	rgArchive   = buildTarGz([]tarEntry{{name: "rg-14.1.0/bin/rg", content: "RIPGREP", exec: true}})
	nodeArchive = buildZip([]tarEntry{
		{name: "bin/node", content: "NODEBIN", exec: true},
		{name: "bin/npm", link: "node"},
	})
)

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestInstallEndToEnd(t *testing.T) {
	root := t.TempDir()
	m := New(root)

	mf := fixtureManifest()
	f := newFixtureServer(t, mf, map[string][]byte{
		"/rg.tar.gz": rgArchive,
		"/node.zip":  nodeArchive,
	}, false)

	events := []Event{}
	m.OnEvent = func(e Event) { events = append(events, e) }

	ctx := context.Background()
	if err := m.Install(ctx, f.srv.URL+"/manifest.json", nil); err != nil {
		t.Fatalf("Install: %v", err)
	}

	lf, err := m.ReadLockfile()
	if err != nil {
		t.Fatal(err)
	}
	if got := lf.Tools["rg"]; got.Version != "14.1.0" || len(got.SHA256) != fixtureDigestLen {
		t.Errorf("locked rg = %+v", got)
	}
	if got := lf.Tools["node"]; got.Version != "22.3.0" || !strings.HasSuffix(got.Path, filepath.Join("tools", "node", "22.3.0")) {
		t.Errorf("locked node = %+v", got)
	}

	rgData, err := os.ReadFile(filepath.Join(root, "tools", "rg", "14.1.0", "bin", "rg"))
	if err != nil || string(rgData) != "RIPGREP" {
		t.Fatalf("rg payload = %q, %v", rgData, err)
	}
	nodeData, err := os.ReadFile(filepath.Join(root, "tools", "node", "22.3.0", "bin", "node"))
	if err != nil || string(nodeData) != "NODEBIN" {
		t.Fatalf("node payload = %q, %v", nodeData, err)
	}

	for shim, want := range map[string]string{
		"rg":   filepath.Join(root, "tools", "rg", "14.1.0", "bin", "rg"),
		"node": filepath.Join(root, "tools", "node", "22.3.0", "bin", "node"),
	} {
		got, err := os.Readlink(filepath.Join(m.ShimDir(), shim))
		if err != nil {
			t.Fatalf("shim %s missing: %v", shim, err)
		}
		if got != want {
			t.Errorf("shim %s target = %q, want %q", shim, got, want)
		}
	}

	npmLink := filepath.Join(m.ShimDir(), "npm")
	if target, err := os.Readlink(npmLink); err != nil {
		t.Fatalf("shim npm missing: %v", err)
	} else if final, err := os.Readlink(target); err != nil || final != "node" {
		t.Errorf("npm chain = %q → %q (%v), want …/bin/npm → node", target, final, err)
	}

	if entries, _ := os.ReadDir(filepath.Join(root, "staging")); len(entries) != 0 {
		t.Errorf("staging not cleaned: %d entries", len(entries))
	}

	kinds := map[EventKind]bool{}
	for _, e := range events {
		kinds[e.Kind] = true
	}
	for _, kind := range []EventKind{EventManifestFetched, EventDownloadStart, EventVerified, EventExtracted, EventActivated, EventDone} {
		if !kinds[kind] {
			t.Errorf("event %s not emitted", kind)
		}
	}

	hitsBefore := f.artifactHits.Load()
	if err := m.Install(ctx, f.srv.URL+"/manifest.json", nil); err != nil {
		t.Fatalf("re-Install: %v", err)
	}
	if got := f.artifactHits.Load(); got != hitsBefore {
		t.Errorf("re-install downloaded again: %d → %d hits", hitsBefore, got)
	}
}

func TestInstallTamperedArtifactFails(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	mf := fixtureManifest()
	f := newFixtureServer(t, mf, map[string][]byte{
		"/rg.tar.gz": rgArchive,
		"/node.zip":  nodeArchive,
	}, true)

	err := m.Install(context.Background(), f.srv.URL+"/manifest.json", []string{"rg"})
	if err == nil {
		t.Fatal("tampered archive accepted")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "tools")); statErr == nil {
		t.Error("tool tree created despite failed verification")
	}
	if entries, _ := os.ReadDir(m.ShimDir()); len(entries) != 0 {
		t.Error("shims created despite failed verification")
	}
	if _, readErr := m.ReadLockfile(); readErr != nil {
		t.Errorf("lockfile unreadable: %v", readErr)
	} else if lf, _ := m.ReadLockfile(); len(lf.Tools) != 0 {
		t.Errorf("lockfile recorded tools despite failure: %+v", lf.Tools)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "staging")); len(entries) != 0 {
		t.Error("staging not cleaned after failure")
	}
}

func TestResolvePlan(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	mf := fixtureManifest()

	plan, err := m.Resolve(mf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 {
		t.Fatalf("plan = %+v, want 2 entries", plan)
	}
	for _, p := range plan {
		if p.Reason != "missing" || p.From != "" {
			t.Errorf("fresh plan entry = %+v", p)
		}
	}

	lf, _ := m.ReadLockfile()
	lf.Tools["rg"] = LockedTool{Version: "13.0.0"}
	if err := m.writeLockfile(lf); err != nil {
		t.Fatal(err)
	}
	plan, err = m.Resolve(mf, []string{"rg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].Reason != "change" || plan[0].From != "13.0.0" || plan[0].To != "14.1.0" {
		t.Fatalf("upgrade plan = %+v", plan)
	}

	if _, err := m.Resolve(mf, []string{"nope"}); err == nil {
		t.Fatal("unknown tool accepted by Resolve")
	}
}

func TestDefaultRootRespectsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	root, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(root, filepath.Join("data", "dhi", "toolchain")) {
		t.Errorf("root = %q", root)
	}

	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	root, err = DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(root, filepath.Join(".local", "share", "dhi", "toolchain")) {
		t.Errorf("root = %q", root)
	}
}
