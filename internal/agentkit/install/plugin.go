package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/skills"
)

// Plugins and marketplaces in the vendor format (ADR-0043): a plugin is a
// folder with .claude-plugin/plugin.json that bundles skills (skills/ by
// default) and MCP servers (.mcp.json at its root by default, or the
// manifest's mcpServers: a path or an inline object). DHI uses those two
// parts; commands, agents and hooks are listed but not run.

// Plugin is one plugin's useful contents.
type Plugin struct {
	Name        string
	Description string
	Version     string
	Dir         string
	SkillDirs   []string           // skill directories it ships
	Servers     []mcpserver.Server // MCP servers it configures
	Unused      []string           // parts DHI does not run: "commands", "agents", "hooks"
	Warnings    []string
}

type pluginManifest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     string          `json:"version"`
	Skills      json.RawMessage `json:"skills"`
	MCPServers  json.RawMessage `json:"mcpServers"`
	Commands    json.RawMessage `json:"commands"`
	Agents      json.RawMessage `json:"agents"`
	Hooks       json.RawMessage `json:"hooks"`
}

// ReadPlugin reads the plugin in dir.
func ReadPlugin(dir string) (*Plugin, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("not a plugin (no .claude-plugin/plugin.json): %w", err)
	}
	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	p := &Plugin{Name: m.Name, Description: m.Description, Version: m.Version, Dir: dir}
	if p.Name == "" {
		p.Name = filepath.Base(dir)
	}
	// Skills: the manifest's path(s), else skills/.
	skillRoots := []string{"skills"}
	if len(m.Skills) > 0 {
		skillRoots = nil
		var one string
		var many []string
		switch {
		case json.Unmarshal(m.Skills, &one) == nil:
			skillRoots = []string{one}
		case json.Unmarshal(m.Skills, &many) == nil:
			skillRoots = many
		}
	}
	for _, r := range skillRoots {
		root, ok := inside(dir, r)
		if !ok {
			p.Warnings = append(p.Warnings, fmt.Sprintf("skills path %q leaves the plugin", r))
			continue
		}
		p.SkillDirs = append(p.SkillDirs, skills.FindAll(root, 3)...)
	}
	// MCP servers: inline object, a path, or .mcp.json at the root.
	var serverData []byte
	serverFile := filepath.Join(dir, ".mcp.json")
	if len(m.MCPServers) > 0 {
		var rel string
		if json.Unmarshal(m.MCPServers, &rel) == nil {
			f, ok := inside(dir, rel)
			if !ok {
				return nil, fmt.Errorf("mcpServers path %q leaves the plugin", rel)
			}
			serverFile = f
		} else {
			serverData = []byte(`{"mcpServers":` + string(m.MCPServers) + `}`)
			serverFile = ""
		}
	}
	if serverData == nil && serverFile != "" {
		if b, err := os.ReadFile(serverFile); err == nil {
			serverData = b
		}
	}
	if serverData != nil {
		servers, warns, err := mcpserver.Parse(serverData, "plugin", serverFile)
		if err != nil {
			return nil, err
		}
		p.Servers = servers
		p.Warnings = append(p.Warnings, warns...)
	}
	for part, raw := range map[string]json.RawMessage{"commands": m.Commands, "agents": m.Agents, "hooks": m.Hooks} {
		if len(raw) > 0 || exists(filepath.Join(dir, part)) {
			p.Unused = append(p.Unused, part)
		}
	}
	sort.Strings(p.Unused)
	return p, nil
}

// Marketplace is a plugin catalog (.claude-plugin/marketplace.json).
type Marketplace struct {
	Name    string
	Source  Source // where the catalog itself came from
	Plugins []PluginEntry
}

// PluginEntry is one listed plugin.
type PluginEntry struct {
	Name        string
	Description string
	Category    string
	Homepage    string
	Source      Source // resolved against the marketplace
}

type marketplaceFile struct {
	Name    string `json:"name"`
	Plugins []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Category    string          `json:"category"`
		Homepage    string          `json:"homepage"`
		Source      json.RawMessage `json:"source"`
	} `json:"plugins"`
}

// ParseMarketplace reads a marketplace.json fetched from src. Entries
// whose source DHI cannot fetch are skipped with a warning.
func ParseMarketplace(data []byte, src Source) (*Marketplace, []string, error) {
	var f marketplaceFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil, fmt.Errorf("marketplace.json: %w", err)
	}
	m := &Marketplace{Name: f.Name, Source: src}
	var warns []string
	for _, p := range f.Plugins {
		ps, err := pluginSource(p.Source, src)
		if err != nil {
			warns = append(warns, fmt.Sprintf("%s: %v", p.Name, err))
			continue
		}
		m.Plugins = append(m.Plugins, PluginEntry{Name: p.Name, Description: p.Description,
			Category: p.Category, Homepage: p.Homepage, Source: ps})
	}
	sort.Slice(m.Plugins, func(i, j int) bool { return m.Plugins[i].Name < m.Plugins[j].Name })
	return m, warns, nil
}

// pluginSource resolves one entry's source: "./plugins/x" (inside the
// marketplace), {"source":"url","url","sha"}, {"source":"git-subdir",
// "url","path","ref","sha"} or {"source":"github","repo"}.
func pluginSource(raw json.RawMessage, market Source) (Source, error) {
	var rel string
	if json.Unmarshal(raw, &rel) == nil {
		sub := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(rel)), "./")
		if strings.HasPrefix(sub, "..") {
			return Source{}, fmt.Errorf("source %q leaves the marketplace", rel)
		}
		out := market
		if out.Path != "" {
			out.Path = filepath.Join(out.Path, sub)
			return out, nil
		}
		out.Subdir = strings.Trim(filepath.ToSlash(filepath.Join(market.Subdir, sub)), "/")
		return out, nil
	}
	var o struct {
		Source string `json:"source"`
		URL    string `json:"url"`
		Repo   string `json:"repo"`
		Path   string `json:"path"`
		Ref    string `json:"ref"`
		SHA    string `json:"sha"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return Source{}, fmt.Errorf("unreadable source")
	}
	ref := o.SHA
	if ref == "" {
		ref = o.Ref
	}
	switch o.Source {
	case "url", "git":
		return Source{URL: o.URL, Ref: ref, Subdir: strings.Trim(o.Path, "/")}, nil
	case "git-subdir":
		return Source{URL: o.URL, Ref: ref, Subdir: strings.Trim(o.Path, "/")}, nil
	case "github":
		return Source{URL: "https://github.com/" + o.Repo + ".git", Ref: ref, Subdir: strings.Trim(o.Path, "/")}, nil
	}
	return Source{}, fmt.Errorf("source type %q is not supported", o.Source)
}

// inside joins rel onto dir and reports whether it stays within dir.
func inside(dir, rel string) (string, bool) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	r, err := filepath.Rel(dir, p)
	return p, err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
