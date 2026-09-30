// Package registry is DHI's signed pack catalog (F-034 Part B,
// ADR-0022): a curated git index of pack manifests + content digests,
// fetched through the hermetic git path, digest-verified before install,
// and cached under .dhi/registry so browsing works offline. There is no
// hosted-service dependency: the index is a git repository the user
// chooses, and its digest pins are the trust anchor (the same
// binary-hashes-artifacts pattern as the toolchain registry).
//
// Only SHA-256 digests exist today; cryptographic signature verification
// is a deliberate follow-on (the index is reviewable, and installs are
// digest-pinned against it).
//
// Follow-on landed: a publisher may sign index.toml with an Ed25519 key
// (detached index.toml.sig). Pin the publisher key in
// .dhi/registry/trusted_keys and every refresh+cached read verifies it;
// see sign.go.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// SchemaVersion is the index schema this build understands.
const SchemaVersion = 1

var (
	sha256Re = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Entry is one pack listed in the index.
type Entry struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Source      string `toml:"source"` // git URL or local path to the pack
	SHA256      string `toml:"sha256"` // lower-hex digest of the pack content
}

// Index is the parsed index.toml.
type Index struct {
	Schema int     `toml:"schema"`
	Packs  []Entry `toml:"pack"`
}

// Parse decodes and validates an index strictly.
func Parse(data []byte) (*Index, error) {
	var ix Index
	md, err := toml.Decode(string(data), &ix)
	if err != nil {
		return nil, fmt.Errorf("registry: parse index: %w", err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("registry: unknown key(s): %s (bump schema?)", strings.Join(keys, ", "))
	}
	if ix.Schema != SchemaVersion {
		return nil, fmt.Errorf("registry: schema %d, want %d", ix.Schema, SchemaVersion)
	}
	seen := map[string]bool{}
	for i := range ix.Packs {
		e := &ix.Packs[i]
		if strings.TrimSpace(e.Name) == "" || strings.TrimSpace(e.Source) == "" {
			return nil, fmt.Errorf("registry: pack entry %d needs a name and source", i)
		}
		if !sha256Re.MatchString(e.SHA256) {
			return nil, fmt.Errorf("registry: pack %q: sha256 must be 64 lowercase hex chars", e.Name)
		}
		if seen[e.Name] {
			return nil, fmt.Errorf("registry: duplicate pack %q", e.Name)
		}
		seen[e.Name] = true
	}
	sort.Slice(ix.Packs, func(i, j int) bool { return ix.Packs[i].Name < ix.Packs[j].Name })
	return &ix, nil
}

// Entry looks up one pack by name.
func (ix *Index) Entry(name string) (Entry, bool) {
	for _, e := range ix.Packs {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Search filters entries whose name or description contains q
// (case-insensitive). An empty query returns everything.
func (ix *Index) Search(q string) []Entry {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return ix.Packs
	}
	var out []Entry
	for _, e := range ix.Packs {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Description), q) {
			out = append(out, e)
		}
	}
	return out
}

// Registry manages the cached index for one workspace.
type Registry struct {
	ws *workspace.Workspace
}

// New returns a registry bound to ws.
func New(ws *workspace.Workspace) *Registry { return &Registry{ws: ws} }

func (r *Registry) dir() string       { return filepath.Join(r.ws.Root, workspace.DirRegistry) }
func (r *Registry) cachePath() string { return filepath.Join(r.dir(), "index.toml") }
func (r *Registry) sigPath() string   { return filepath.Join(r.dir(), "index.toml.sig") }
func (r *Registry) srcPath() string   { return filepath.Join(r.dir(), "source") }
func (r *Registry) stampPath() string { return filepath.Join(r.dir(), "fetched_at") }

// Source returns the last registry source, if one was set.
func (r *Registry) Source() (string, bool) {
	data, err := os.ReadFile(r.srcPath())
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(data))
	return s, s != ""
}

// FetchedAt returns when the cache was last refreshed.
func (r *Registry) FetchedAt() (time.Time, bool) {
	data, err := os.ReadFile(r.stampPath())
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// index reads the cached index. A missing cache is a named error (run a
// refresh first) — browsing never silently pretends to be empty. When a
// publisher key is pinned, the cached index must carry a signature that
// verifies against it (a tampered or unsigned cache refuses by name).
func (r *Registry) index() (*Index, error) {
	data, err := os.ReadFile(r.cachePath())
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("registry: no cached index — refresh from a source first")
	}
	if err != nil {
		return nil, fmt.Errorf("registry: read cache: %w", err)
	}
	keys, err := r.decodedKeys()
	if err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		sig, serr := os.ReadFile(r.sigPath())
		if serr != nil {
			return nil, fmt.Errorf("registry: trusted key pinned but cached index is unsigned — refresh from a signed source")
		}
		if verr := VerifyIndex(data, string(sig), keys); verr != nil {
			return nil, verr
		}
	}
	return Parse(data)
}

// Browse lists the cached entries sorted by name.
func (r *Registry) Browse() ([]Entry, error) {
	ix, err := r.index()
	if err != nil {
		return nil, err
	}
	return ix.Packs, nil
}

// Search filters the cached entries.
func (r *Registry) Search(q string) ([]Entry, error) {
	ix, err := r.index()
	if err != nil {
		return nil, err
	}
	return ix.Search(q), nil
}

// Refresh fetches the index from source (git URL or local path) through
// the hermetic git path, validates it, and caches it (digests included)
// for offline browsing. The source is recorded so later refreshes can
// prefill it.
func (r *Registry) Refresh(ctx context.Context, source string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return fmt.Errorf("registry: source required")
	}
	dir, cleanup, err := resolve(ctx, source)
	if err != nil {
		return err
	}
	defer cleanup()
	data, err := os.ReadFile(filepath.Join(dir, "index.toml"))
	if err != nil {
		return fmt.Errorf("registry: read index.toml: %w", err)
	}
	if _, err := Parse(data); err != nil {
		return err
	}
	// Signature policy (F-034 follow-on): with a pinned publisher key the
	// source MUST ship a verifying index.toml.sig; without one the
	// registry stays in digest-only mode.
	keys, err := r.decodedKeys()
	if err != nil {
		return err
	}
	var sig []byte
	if raw, serr := os.ReadFile(filepath.Join(dir, sigFile)); serr == nil {
		sig = raw
	}
	if len(keys) > 0 {
		if sig == nil {
			return fmt.Errorf("registry: a publisher key is pinned but the source index is unsigned — sign it or untrust the key")
		}
		if verr := VerifyIndex(data, string(sig), keys); verr != nil {
			return verr
		}
	}
	if err := os.MkdirAll(r.dir(), 0o755); err != nil {
		return fmt.Errorf("registry: mkdir: %w", err)
	}
	if err := writeAtomic(r.cachePath(), data); err != nil {
		return err
	}
	if sig != nil {
		if err := writeAtomic(r.sigPath(), sig); err != nil {
			return err
		}
	} else {
		_ = os.Remove(r.sigPath())
	}
	if err := writeAtomic(r.srcPath(), []byte(source+"\n")); err != nil {
		return err
	}
	return writeAtomic(r.stampPath(), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
}

// Install fetches the pack named by the cached index entry, verifies its
// content digest against the pinned index value, and only then installs.
// A digest mismatch refuses by name and installs nothing (acceptance 2).
func (r *Registry) Install(ctx context.Context, name string, in *pack.Installer) (*pack.Result, error) {
	ix, err := r.index()
	if err != nil {
		return nil, err
	}
	entry, ok := ix.Entry(name)
	if !ok {
		return nil, fmt.Errorf("registry: pack %q is not in the index", name)
	}
	dir, cleanup, err := resolve(ctx, entry.Source)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	got, err := Digest(dir)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(got, entry.SHA256) {
		return nil, fmt.Errorf("registry: pack %q digest mismatch (index %s, got %s) — refusing to install",
			name, entry.SHA256, got)
	}
	return in.InstallDir(dir, entry.Source)
}

// Digest is the canonical SHA-256 of a pack directory's contents: files
// sorted by slash-relative path, each folded in as path NUL content NUL.
// .git is ignored so a clone and a checked-out tree agree.
func Digest(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("registry: digest walk: %w", err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", fmt.Errorf("registry: digest read %s: %w", rel, err)
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func resolve(ctx context.Context, source string) (string, func(), error) {
	if isURL(source) {
		c, err := os.MkdirTemp("", "dhi-registry-*")
		if err != nil {
			return "", nil, err
		}
		dst := filepath.Join(c, "index")
		if _, err := gitcore.Clone(ctx, source, dst); err != nil {
			_ = os.RemoveAll(c)
			return "", nil, err
		}
		return dst, func() { _ = os.RemoveAll(c) }, nil
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return "", nil, fmt.Errorf("registry: source %s is not a directory", source)
	}
	return source, func() {}, nil
}

func isURL(s string) bool {
	for _, p := range []string{"http://", "https://", "git://", "ssh://", "git@"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
