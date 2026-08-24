// Package pack installs agent marketplace packs (F-008): a versioned
// directory of manifests fetched from a local path or git URL, validated
// in full before the first file lands, and tracked in
// .dhi/marketplace.json so uninstall removes exactly what install wrote.
package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// SchemaVersion is the pack.toml schema this build understands.
const SchemaVersion = 1

// ProvenanceFile records installed packs under .dhi/.
const ProvenanceFile = ".dhi/marketplace.json"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Spec is the parsed pack.toml.
type Spec struct {
	Schema      int
	Name        string
	Version     string
	Description string
	Agents      []string // repo-relative manifest paths, sorted
}

type specFile struct {
	Schema      int      `toml:"schema"`
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	Description string   `toml:"description"`
	Agents      []string `toml:"agents"`
}

// ReadSpec decodes and validates <dir>/pack.toml strictly.
func ReadSpec(dir string) (*Spec, error) {
	data, err := os.ReadFile(filepath.Join(dir, "pack.toml"))
	if err != nil {
		return nil, fmt.Errorf("pack: read pack.toml: %w", err)
	}
	var f specFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, fmt.Errorf("pack: parse pack.toml: %w", err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("pack: unknown key(s): %s (bump schema?)", strings.Join(keys, ", "))
	}
	if f.Schema != SchemaVersion {
		return nil, fmt.Errorf("pack: schema %d, want %d", f.Schema, SchemaVersion)
	}
	if !nameRe.MatchString(f.Name) {
		return nil, fmt.Errorf("pack: bad name %q (lowercase [a-z0-9._-])", f.Name)
	}
	if len(f.Agents) == 0 {
		return nil, fmt.Errorf("pack: no agents listed")
	}
	s := &Spec{Schema: f.Schema, Name: f.Name, Version: f.Version,
		Description: f.Description, Agents: f.Agents}
	sort.Strings(s.Agents)
	return s, nil
}

// Result summarizes one successful install/update.
type Result struct {
	Pack    string
	Version string
	Agents  []string // ids written, sorted
	Updated bool     // provenance entry existed before
}

// Installer writes packs into ws's roster.
type Installer struct {
	WS *workspace.Workspace
}

// provenance is the on-disk marketplace.json shape.
type provenance struct {
	Schema int                `json:"schema"`
	Packs  map[string]PackRec `json:"packs"`
}

// PackRec is one installed pack's record.
type PackRec struct {
	Source      string    `json:"source"`
	Version     string    `json:"version,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
	Agents      []string  `json:"agents"`
}

func (in *Installer) provPath() string {
	return filepath.Join(in.WS.Root, ProvenanceFile)
}

func (in *Installer) readProvenance() (*provenance, error) {
	p := &provenance{Schema: 1, Packs: map[string]PackRec{}}
	data, err := os.ReadFile(in.provPath())
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("pack: parse %s: %w", ProvenanceFile, err)
	}
	if p.Packs == nil {
		p.Packs = map[string]PackRec{}
	}
	return p, nil
}

func (in *Installer) writeProvenance(p *provenance) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := in.provPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".marketplace-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Install resolves source (local dir or git URL), validates every listed
// manifest, then installs. See F-008 for the conflict rule.
func (in *Installer) Install(ctx context.Context, source string) (*Result, error) {
	dir := source
	tmpClone := ""
	if isURL(source) {
		c, err := os.MkdirTemp("", "dhi-pack-*")
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(c, "pack")
		if _, err := gitcore.Clone(ctx, source, dst); err != nil {
			os.RemoveAll(c)
			return nil, err
		}
		tmpClone = c
		dir = dst
	} else {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("pack: source %s is not a directory", source)
		}
	}
	if tmpClone != "" {
		defer os.RemoveAll(tmpClone)
	}

	spec, err := ReadSpec(dir)
	if err != nil {
		return nil, err
	}

	rosterDir := filepath.Join(in.WS.Root, workspace.DirAgents)
	agents := make([]*manifest.Agent, 0, len(spec.Agents))
	for _, rel := range spec.Agents {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("pack: agent path %q escapes the pack", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		id := strings.TrimSuffix(filepath.Base(rel), ".toml")
		a, err := manifest.Parse(id, data)
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		agents = append(agents, a)
	}

	prov, err := in.readProvenance()
	if err != nil {
		return nil, err
	}
	prev, updating := prov.Packs[spec.Name]
	owned := map[string]bool{}
	for _, id := range prev.Agents {
		owned[id] = true
	}
	var conflicts []string
	for _, a := range agents {
		path := filepath.Join(rosterDir, a.ID+".toml")
		if _, err := os.Stat(path); err == nil && !owned[a.ID] {
			conflicts = append(conflicts, a.ID)
		}
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("pack: agent(s) %s already exist and belong to another source",
			strings.Join(conflicts, ", "))
	}

	if err := os.MkdirAll(rosterDir, 0o755); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(agents))
	for _, a := range agents {
		if err := manifest.WriteFile(rosterDir, a); err != nil {
			return nil, err
		}
		ids = append(ids, a.ID)
	}
	sort.Strings(ids)
	prov.Packs[spec.Name] = PackRec{
		Source:      source,
		Version:     spec.Version,
		InstalledAt: time.Now(),
		Agents:      ids,
	}
	if err := in.writeProvenance(prov); err != nil {
		return nil, err
	}
	return &Result{Pack: spec.Name, Version: spec.Version, Agents: ids, Updated: updating}, nil
}

// Uninstall removes exactly the recorded agents of packName; unknown
// packs error. Files already deleted by hand are tolerated.
func (in *Installer) Uninstall(packName string) error {
	prov, err := in.readProvenance()
	if err != nil {
		return err
	}
	rec, ok := prov.Packs[packName]
	if !ok {
		return fmt.Errorf("pack: %q not installed", packName)
	}
	rosterDir := filepath.Join(in.WS.Root, workspace.DirAgents)
	for _, id := range rec.Agents {
		if err := os.Remove(filepath.Join(rosterDir, id+".toml")); err != nil &&
			!os.IsNotExist(err) {
			return err
		}
	}
	delete(prov.Packs, packName)
	return in.writeProvenance(prov)
}

// Installed lists installed pack names sorted with their agent counts.
func (in *Installer) Installed() ([]string, error) {
	prov, err := in.readProvenance()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(prov.Packs))
	for name := range prov.Packs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func isURL(s string) bool {
	for _, p := range []string{"http://", "https://", "git://", "ssh://", "git@"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Records returns a copy of the installed pack records keyed by name,
// for UI listing.
func (in *Installer) Records() (map[string]PackRec, error) {
	prov, err := in.readProvenance()
	if err != nil {
		return nil, err
	}
	out := make(map[string]PackRec, len(prov.Packs))
	for k, v := range prov.Packs {
		out[k] = v
	}
	return out, nil
}
