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

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// SchemaVersion is the pack.toml schema this build understands. Schema 2
// adds roles, skills, standards and mcp_servers (F-034/ADR-0022).
const SchemaVersion = 2

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
	Workflows   []string // repo-relative workflow paths, sorted (F-031/ADR-0022)
	Roles       []string // repo-relative role card paths, sorted (F-034)
	Skills      []string // repo-relative skill doc paths, sorted (F-034)
	Standards   []string // repo-relative rules-fragment paths, sorted (F-034)
	MCPServers  []string // repo-relative MCP server card paths, sorted (F-034)
}

type specFile struct {
	Schema      int      `toml:"schema"`
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	Description string   `toml:"description"`
	Agents      []string `toml:"agents"`
	Workflows   []string `toml:"workflows"`
	Roles       []string `toml:"roles"`
	Skills      []string `toml:"skills"`
	Standards   []string `toml:"standards"`
	MCPServers  []string `toml:"mcp_servers"`
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
	if len(f.Agents) == 0 && len(f.Workflows) == 0 && len(f.Roles) == 0 &&
		len(f.Skills) == 0 && len(f.Standards) == 0 && len(f.MCPServers) == 0 {
		return nil, fmt.Errorf("pack: lists nothing installable (agents, workflows, roles, skills, standards, mcp_servers)")
	}
	s := &Spec{Schema: f.Schema, Name: f.Name, Version: f.Version,
		Description: f.Description, Agents: f.Agents, Workflows: f.Workflows,
		Roles: f.Roles, Skills: f.Skills, Standards: f.Standards, MCPServers: f.MCPServers}
	sort.Strings(s.Agents)
	sort.Strings(s.Workflows)
	sort.Strings(s.Roles)
	sort.Strings(s.Skills)
	sort.Strings(s.Standards)
	sort.Strings(s.MCPServers)
	return s, nil
}

// Result summarizes one successful install/update.
type Result struct {
	Pack       string
	Version    string
	Agents     []string // ids written, sorted
	Workflows  []string // workflow slugs written, sorted
	Roles      []string // role slugs written, sorted
	Skills     []string // skill slugs written, sorted
	Standards  []string // workspace rule lines added, sorted
	MCPServers []string // MCP server slugs written, sorted
	Updated    bool     // provenance entry existed before
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
	Workflows   []string  `json:"workflows,omitempty"`
	Roles       []string  `json:"roles,omitempty"`
	Skills      []string  `json:"skills,omitempty"`
	Standards   []string  `json:"standards,omitempty"`
	MCPServers  []string  `json:"mcp_servers,omitempty"`
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
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
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
			_ = os.RemoveAll(c)
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
		defer func() { _ = os.RemoveAll(tmpClone) }()
	}
	return in.InstallDir(dir, source)
}

// InstallDir installs an already-resolved pack directory (a caller may
// have cloned + digest-verified it, e.g. the signed registry, F-034).
// source is recorded in provenance for uninstall/audit.
func (in *Installer) InstallDir(dir, source string) (*Result, error) {
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

	// Workflows ship as .dhi/workflows/<slug>.toml files, validated in
	// full before any file lands (ADR-0022 third-party trust).
	wfDir := filepath.Join(in.WS.Root, filepath.FromSlash(workflow.Dir))
	type wfItem struct {
		slug string
		data []byte
	}
	var wfs []wfItem
	for _, rel := range spec.Workflows {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("pack: workflow path %q escapes the pack", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		slug := strings.TrimSuffix(filepath.Base(rel), ".toml")
		if _, err := workflow.Parse(slug, data); err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		wfs = append(wfs, wfItem{slug: slug, data: data})
	}

	// Roles and skills validate through the library's own strict readers
	// before anything lands; they install to .dhi/roles and .dhi/skills.
	type roleItem struct {
		slug string
		role *library.Role
	}
	var roles []roleItem
	for _, rel := range spec.Roles {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("pack: role path %q escapes the pack", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		slug := strings.TrimSuffix(filepath.Base(rel), ".toml")
		r, err := library.ParseRole(slug, data)
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		roles = append(roles, roleItem{slug: slug, role: r})
	}
	type skillItem struct {
		slug  string
		skill *library.Skill
	}
	var skills []skillItem
	for _, rel := range spec.Skills {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("pack: skill path %q escapes the pack", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		slug := strings.TrimSuffix(filepath.Base(rel), ".md")
		k, err := library.ParseSkill(slug, data)
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		skills = append(skills, skillItem{slug: slug, skill: k})
	}

	// MCP server cards install to .dhi/mcp; validated before any write.
	type mcpItem struct {
		slug string
		srv  *mcpserver.Server
	}
	var mcps []mcpItem
	for _, rel := range spec.MCPServers {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("pack: mcp server path %q escapes the pack", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		slug := strings.TrimSuffix(filepath.Base(rel), ".toml")
		srv, err := mcpserver.Parse(slug, data)
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		mcps = append(mcps, mcpItem{slug: slug, srv: srv})
	}

	// Standards ship as rules fragments; install appends the new lines to
	// the workspace layer, so the pack owns exactly the lines it added.
	var stdRules []string
	for _, rel := range spec.Standards {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("pack: standards path %q escapes the pack", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		var frag struct {
			Rules []string `toml:"rules"`
		}
		md, err := toml.Decode(string(data), &frag)
		if err != nil {
			return nil, fmt.Errorf("pack: %s: %w", rel, err)
		}
		if und := md.Undecoded(); len(und) > 0 {
			return nil, fmt.Errorf("pack: %s: unknown key(s) in rules fragment", rel)
		}
		for _, r := range frag.Rules {
			if r = strings.TrimSpace(r); r != "" {
				stdRules = append(stdRules, r)
			}
		}
	}
	stdRules = uniqueStrings(stdRules)

	prov, err := in.readProvenance()
	if err != nil {
		return nil, err
	}
	prev, updating := prov.Packs[spec.Name]
	owned := ownedSet(prev.Agents, prev.Workflows, prev.Roles, prev.Skills, prev.MCPServers)
	roleDir := filepath.Join(in.WS.Root, workspace.DirRoles)
	skillDir := filepath.Join(in.WS.Root, workspace.DirSkills)
	mcpDir := filepath.Join(in.WS.Root, workspace.DirMCP)
	var conflicts []string
	for _, a := range agents {
		path := filepath.Join(rosterDir, a.ID+".toml")
		if _, err := os.Stat(path); err == nil && !owned["agent/"+a.ID] {
			conflicts = append(conflicts, "agent "+a.ID)
		}
	}
	for _, w := range wfs {
		path := filepath.Join(wfDir, w.slug+".toml")
		if _, err := os.Stat(path); err == nil && !owned["workflow/"+w.slug] {
			conflicts = append(conflicts, "workflow "+w.slug)
		}
	}
	for _, r := range roles {
		path := filepath.Join(roleDir, r.slug+".toml")
		if _, err := os.Stat(path); err == nil && !owned["role/"+r.slug] {
			conflicts = append(conflicts, "role "+r.slug)
		}
	}
	for _, k := range skills {
		path := filepath.Join(skillDir, k.slug+".md")
		if _, err := os.Stat(path); err == nil && !owned["skill/"+k.slug] {
			conflicts = append(conflicts, "skill "+k.slug)
		}
	}
	for _, m := range mcps {
		path := filepath.Join(mcpDir, m.slug+".toml")
		if _, err := os.Stat(path); err == nil && !owned["mcp/"+m.slug] {
			conflicts = append(conflicts, "mcp server "+m.slug)
		}
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("pack: %s already exist and belong to another source",
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
	wfSlugs := make([]string, 0, len(wfs))
	if len(wfs) > 0 {
		if err := os.MkdirAll(wfDir, 0o755); err != nil {
			return nil, err
		}
		for _, w := range wfs {
			if err := os.WriteFile(filepath.Join(wfDir, w.slug+".toml"), w.data, 0o644); err != nil {
				return nil, fmt.Errorf("pack: write workflow %s: %w", w.slug, err)
			}
			wfSlugs = append(wfSlugs, w.slug)
		}
		sort.Strings(wfSlugs)
	}
	roleSlugs := make([]string, 0, len(roles))
	for _, r := range roles {
		if err := library.WriteRole(in.WS, r.role); err != nil {
			return nil, fmt.Errorf("pack: write role %s: %w", r.slug, err)
		}
		roleSlugs = append(roleSlugs, r.slug)
	}
	sort.Strings(roleSlugs)
	skillSlugs := make([]string, 0, len(skills))
	for _, k := range skills {
		if err := library.WriteSkill(in.WS, k.skill); err != nil {
			return nil, fmt.Errorf("pack: write skill %s: %w", k.slug, err)
		}
		skillSlugs = append(skillSlugs, k.slug)
	}
	sort.Strings(skillSlugs)
	mcpSlugs := make([]string, 0, len(mcps))
	for _, m := range mcps {
		if err := mcpserver.Write(in.WS.Root, m.srv); err != nil {
			return nil, fmt.Errorf("pack: write mcp server %s: %w", m.slug, err)
		}
		mcpSlugs = append(mcpSlugs, m.slug)
	}
	sort.Strings(mcpSlugs)

	// Merge the standards rules into the workspace layer, preserving the
	// team/agent layers the user owns. Only rules not already present are
	// recorded, so uninstall is exact.
	addedStd, err := mergeStandards(in.WS.Root, stdRules)
	if err != nil {
		return nil, err
	}

	prov.Packs[spec.Name] = PackRec{
		Source:      source,
		Version:     spec.Version,
		InstalledAt: time.Now(),
		Agents:      ids,
		Workflows:   wfSlugs,
		Roles:       roleSlugs,
		Skills:      skillSlugs,
		Standards:   addedStd,
		MCPServers:  mcpSlugs,
	}
	if err := in.writeProvenance(prov); err != nil {
		return nil, err
	}
	return &Result{Pack: spec.Name, Version: spec.Version, Agents: ids, Workflows: wfSlugs,
		Roles: roleSlugs, Skills: skillSlugs, Standards: addedStd, MCPServers: mcpSlugs,
		Updated: updating}, nil
}

// mergeStandards appends rules to the workspace standards layer and
// returns the rules actually added (already-present rules are skipped).
func mergeStandards(root string, rules []string) ([]string, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	snap, err := standards.Inspect(root)
	if err != nil {
		return nil, fmt.Errorf("pack: standards: %w", err)
	}
	have := map[string]bool{}
	for _, r := range snap.Workspace {
		have[r] = true
	}
	var added []string
	workspace := append([]string(nil), snap.Workspace...)
	for _, r := range rules {
		if have[r] {
			continue
		}
		have[r] = true
		workspace = append(workspace, r)
		added = append(added, r)
	}
	if len(added) == 0 {
		return nil, nil
	}
	if err := standards.Save(root, workspace, snap.Teams, snap.Agents); err != nil {
		return nil, fmt.Errorf("pack: standards: %w", err)
	}
	sort.Strings(added)
	return added, nil
}

// unmergeStandards removes exactly the recorded rules from the workspace
// layer, preserving everything else.
func unmergeStandards(root string, rules []string) error {
	if len(rules) == 0 {
		return nil
	}
	snap, err := standards.Inspect(root)
	if err != nil {
		return fmt.Errorf("pack: standards: %w", err)
	}
	drop := map[string]bool{}
	for _, r := range rules {
		drop[r] = true
	}
	workspace := make([]string, 0, len(snap.Workspace))
	for _, r := range snap.Workspace {
		if !drop[r] {
			workspace = append(workspace, r)
		}
	}
	return standards.Save(root, workspace, snap.Teams, snap.Agents)
}

// ownedSet builds the owned keys for every recorded kind.
func ownedSet(agents, workflows, roles, skills, mcps []string) map[string]bool {
	owned := map[string]bool{}
	for _, v := range agents {
		owned["agent/"+v] = true
	}
	for _, v := range workflows {
		owned["workflow/"+v] = true
	}
	for _, v := range roles {
		owned["role/"+v] = true
	}
	for _, v := range skills {
		owned["skill/"+v] = true
	}
	for _, v := range mcps {
		owned["mcp/"+v] = true
	}
	return owned
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Uninstall removes exactly the recorded files/lines of packName; unknown
// packs error. Entries already deleted by hand are tolerated.
func (in *Installer) Uninstall(packName string) error {
	prov, err := in.readProvenance()
	if err != nil {
		return err
	}
	rec, ok := prov.Packs[packName]
	if !ok {
		return fmt.Errorf("pack: %q not installed", packName)
	}
	remove := func(dir, name string) error {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	rosterDir := filepath.Join(in.WS.Root, workspace.DirAgents)
	for _, id := range rec.Agents {
		if err := remove(rosterDir, id+".toml"); err != nil {
			return err
		}
	}
	wfDir := filepath.Join(in.WS.Root, filepath.FromSlash(workflow.Dir))
	for _, slug := range rec.Workflows {
		if err := remove(wfDir, slug+".toml"); err != nil {
			return err
		}
	}
	roleDir := filepath.Join(in.WS.Root, workspace.DirRoles)
	for _, slug := range rec.Roles {
		if err := remove(roleDir, slug+".toml"); err != nil {
			return err
		}
	}
	skillDir := filepath.Join(in.WS.Root, workspace.DirSkills)
	for _, slug := range rec.Skills {
		if err := remove(skillDir, slug+".md"); err != nil {
			return err
		}
	}
	for _, slug := range rec.MCPServers {
		if err := mcpserver.Delete(in.WS.Root, slug); err != nil {
			return err
		}
	}
	if err := unmergeStandards(in.WS.Root, rec.Standards); err != nil {
		return err
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
