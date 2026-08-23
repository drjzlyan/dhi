package toolchain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// maxManifestBytes caps manifest size; it is a small pinned document.
const maxManifestBytes = 4 << 20

// EventKind names a lifecycle stage of the install pipeline. The
// bootstrap surface subscribes to these; granularity is stage-level so
// behavior is deterministic and fully testable headlessly.
type EventKind string

// Pipeline events.
const (
	EventManifestFetched EventKind = "manifest_fetched"
	EventResolved        EventKind = "resolved"
	EventDownloadStart   EventKind = "download_start"
	EventDownloadDone    EventKind = "download_done"
	EventVerified        EventKind = "verified"
	EventExtracted       EventKind = "extracted"
	EventActivated       EventKind = "activated"
	EventToolDone        EventKind = "tool_done"
	EventDone            EventKind = "done"
)

// Event is one pipeline progress notification.
type Event struct {
	Kind   EventKind
	Tool   string
	Detail string
}

// Manager drives the resolve→download→verify→extract→activate pipeline
// against an XDG-isolated root (default ~/.local/share/dhi/toolchain).
type Manager struct {
	root    string
	client  *http.Client
	now     func() time.Time
	OnEvent func(Event)
}

// New returns a Manager rooted at dir.
func New(dir string) *Manager {
	return &Manager{
		root:   dir,
		client: &http.Client{},
		now:    time.Now,
	}
}

// DefaultRoot is the XDG-isolated toolchain prefix:
// $XDG_DATA_HOME/dhi/toolchain, else ~/.local/share/dhi/toolchain.
func DefaultRoot() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("toolchain: locate prefix: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "dhi", "toolchain"), nil
}

// Root is the toolchain prefix directory.
func (m *Manager) Root() string { return m.root }

// ShimDir holds the per-tool executable links prepended to the PATH of
// DHI's child processes only — never the user's shell.
func (m *Manager) ShimDir() string { return filepath.Join(m.root, "bin") }

func (m *Manager) emit(e Event) {
	if m.OnEvent != nil {
		m.OnEvent(e)
	}
}

func (m *Manager) toolDir(name, version string) string {
	return filepath.Join(m.root, "tools", name, version)
}

// PlanEntry is one pending action produced by Resolve.
type PlanEntry struct {
	Tool   string
	From   string // locked version, "" when absent
	To     string
	Reason string // "missing" | "change"
}

// Resolve diffs the manifest against installed state for names (all
// manifest tools when empty), returning the work Install would do.
func (m *Manager) Resolve(mf *Manifest, names []string) ([]PlanEntry, error) {
	lf, err := m.ReadLockfile()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		for name := range mf.Tools {
			names = append(names, name)
		}
	}
	var plan []PlanEntry
	for _, name := range names {
		tool, ok := mf.Tools[name]
		if !ok {
			return nil, fmt.Errorf("toolchain: unknown tool %q", name)
		}
		if _, err := mf.Spec(name); err != nil {
			return nil, err
		}
		cur := lf.Tools[name].Version
		switch {
		case cur == "":
			plan = append(plan, PlanEntry{Tool: name, To: tool.Version, Reason: "missing"})
		case cur != tool.Version:
			plan = append(plan, PlanEntry{Tool: name, From: cur, To: tool.Version, Reason: "change"})
		}
	}
	return plan, nil
}

// FetchManifest downloads, parses, and validates the registry manifest.
func (m *Manager) FetchManifest(ctx context.Context, url string) (*Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("toolchain: manifest: %w", err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("toolchain: fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("toolchain: fetch manifest: status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("toolchain: fetch manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("toolchain: manifest exceeds %d bytes", maxManifestBytes)
	}
	mf, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	m.emit(Event{Kind: EventManifestFetched, Detail: url})
	return mf, nil
}

// Install runs the full pipeline for names (all tools when empty) using
// a remotely fetched manifest. Every artifact is checksum-verified
// before extraction; activation is an atomic rename; the lockfile is
// written only after all requested tools succeed.
func (m *Manager) Install(ctx context.Context, manifestURL string, names []string) error {
	mf, err := m.FetchManifest(ctx, manifestURL)
	if err != nil {
		return err
	}
	return m.install(ctx, mf, names)
}

// InstallEmbedded runs the pipeline against the in-binary registry
// manifest — the default production path (pins ship with releases).
// names selects tools (all when empty).
func (m *Manager) InstallEmbedded(ctx context.Context, names []string) error {
	mf, err := Embedded()
	if err != nil {
		return err
	}
	m.emit(Event{Kind: EventManifestFetched, Detail: "embedded registry"})
	return m.install(ctx, mf, names)
}

func (m *Manager) install(ctx context.Context, mf *Manifest, names []string) error {
	plan, err := m.Resolve(mf, names)
	if err != nil {
		return err
	}
	m.emit(Event{Kind: EventResolved, Detail: fmt.Sprintf("%d action(s)", len(plan))})

	stagingBase := filepath.Join(m.root, "staging")
	if err := os.MkdirAll(stagingBase, 0o755); err != nil {
		return fmt.Errorf("toolchain: staging: %w", err)
	}

	lf, err := m.ReadLockfile()
	if err != nil {
		return err
	}

	for _, entry := range plan {
		tool := mf.Tools[entry.Tool]
		spec, err := mf.Spec(entry.Tool)
		if err != nil {
			return err
		}
		stageDir, err := os.MkdirTemp(stagingBase, entry.Tool+"-"+entry.To+"-")
		if err != nil {
			return fmt.Errorf("toolchain: staging: %w", err)
		}
		err = m.installOne(ctx, entry, tool, spec, stageDir)
		os.RemoveAll(stageDir)
		if err != nil {
			return err
		}
		lf.Tools[entry.Tool] = LockedTool{
			Version: entry.To,
			SHA256:  spec.SHA256,
			Path:    filepath.Join("tools", entry.Tool, entry.To),
		}
		m.emit(Event{Kind: EventToolDone, Tool: entry.Tool})
	}
	if len(plan) == 0 {
		m.emit(Event{Kind: EventDone, Detail: "up to date"})
		return nil
	}
	if err := m.writeLockfile(lf); err != nil {
		return err
	}
	m.emit(Event{Kind: EventDone, Detail: fmt.Sprintf("%d installed", len(plan))})
	return nil
}

func (m *Manager) installOne(ctx context.Context, entry PlanEntry, tool Tool, spec PlatformSpec, stageDir string) error {
	archivePath := filepath.Join(stageDir, "artifact."+string(spec.Format))
	m.emit(Event{Kind: EventDownloadStart, Tool: entry.Tool})
	if err := m.download(ctx, spec.URL, archivePath); err != nil {
		return err
	}
	m.emit(Event{Kind: EventDownloadDone, Tool: entry.Tool})
	if err := VerifyHash(archivePath, spec.SHA256); err != nil {
		return err
	}
	m.emit(Event{Kind: EventVerified, Tool: entry.Tool})

	extractDir := filepath.Join(stageDir, "root")
	if err := Extract(archivePath, spec.Format, spec.Strip, extractDir); err != nil {
		return err
	}
	m.emit(Event{Kind: EventExtracted, Tool: entry.Tool})

	target := m.toolDir(entry.Tool, entry.To)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("toolchain: activate: %w", err)
	}
	_ = os.RemoveAll(target)
	if err := os.Rename(extractDir, target); err != nil {
		return fmt.Errorf("toolchain: activate %s@%s: %w", entry.Tool, entry.To, err)
	}
	m.emit(Event{Kind: EventActivated, Tool: entry.Tool})

	if err := m.linkShims(entry.Tool, tool, target, spec.BinDir); err != nil {
		return err
	}
	return nil
}

func (m *Manager) download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("toolchain: download: %w", err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("toolchain: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("toolchain: download %s: status %s", filepath.Base(dst), resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("toolchain: download: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("toolchain: download: %w", err)
	}
	return f.Close()
}

// linkShims exposes each shim name as a symlink in the shim dir pointing
// at the freshly activated version's binary.
func (m *Manager) linkShims(name string, tool Tool, versionDir, binDir string) error {
	shimDir := m.ShimDir()
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return fmt.Errorf("toolchain: shims: %w", err)
	}
	for _, shim := range tool.Shims {
		bin := filepath.Join(versionDir, binDir, shim)
		if _, err := os.Stat(bin); err != nil {
			return fmt.Errorf("toolchain: %s: shim target %s missing after extract",
				name, filepath.Join(binDir, shim))
		}
		link := filepath.Join(shimDir, shim)
		_ = os.Remove(link)
		if err := os.Symlink(bin, link); err != nil {
			return fmt.Errorf("toolchain: shim %s: %w", shim, err)
		}
	}
	return nil
}
