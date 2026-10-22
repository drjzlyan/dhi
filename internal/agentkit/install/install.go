package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/skills"
)

// DefaultMarketplace is where the vendors publish their official plugins
// (Atlassian, Slack, Zoom, Linear, Notion, GitHub, …).
const DefaultMarketplace = "https://github.com/anthropics/claude-plugins-official.git"

// Scope is where an install lands.
type Scope struct {
	Name      string // "this workspace" or "every workspace"
	SkillsDir string // <ws>/.dhi/skills or <config>/dhi/skills
	MCPFile   string // <ws>/.dhi/mcp.json or <config>/dhi/mcp.json
	LockPath  string // installed.toml next to them
}

// Fetcher clones url at ref into dst and returns the commit (gitcore).
type Fetcher func(ctx context.Context, url, ref, dst string) (string, error)

// Bundle is what a source holds, fetched and parsed, before anything is
// written: the preview the user confirms.
type Bundle struct {
	Source      Source
	Commit      string
	Dir         string
	Plugin      *Plugin      // the source is a plugin
	Marketplace *Marketplace // the source is a marketplace: pick a plugin
	Skills      []Candidate
	Servers     []mcpserver.Server
	Warnings    []string
	cleanup     func()
}

// Candidate is one skill a bundle offers.
type Candidate struct {
	Skill   *skills.Skill
	Dir     string
	Scripts []string // files that would run (scripts/ and executables), relative
}

// Close removes the bundle's temporary checkout.
func (b *Bundle) Close() {
	if b != nil && b.cleanup != nil {
		b.cleanup()
		b.cleanup = nil
	}
}

// Preview fetches src (a clone under tmp, or a local folder) and lists
// what it would install.
func Preview(ctx context.Context, src Source, fetch Fetcher, tmp string) (*Bundle, error) {
	b := &Bundle{Source: src}
	root := src.Path
	if src.URL != "" {
		if fetch == nil {
			return nil, fmt.Errorf("no git fetcher")
		}
		dir, err := os.MkdirTemp(tmp, "dhi-install-*")
		if err != nil {
			return nil, err
		}
		b.cleanup = func() { _ = os.RemoveAll(dir) }
		commit, err := fetch(ctx, src.URL, src.Ref, dir)
		if err != nil {
			b.Close()
			return nil, err
		}
		b.Commit, root = commit, dir
	}
	if src.Subdir != "" {
		sub, ok := inside(root, src.Subdir)
		if !ok {
			b.Close()
			return nil, fmt.Errorf("%s leaves the repository", src.Subdir)
		}
		root = sub
	}
	if _, err := os.Stat(root); err != nil {
		b.Close()
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	b.Dir = root
	switch {
	case exists(filepath.Join(root, ".claude-plugin", "plugin.json")):
		p, err := ReadPlugin(root)
		if err != nil {
			b.Close()
			return nil, err
		}
		b.Plugin, b.Servers, b.Warnings = p, p.Servers, p.Warnings
		b.Skills, b.Warnings = candidates(p.SkillDirs, b.Warnings)
	case exists(filepath.Join(root, ".claude-plugin", "marketplace.json")):
		data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "marketplace.json"))
		if err != nil {
			b.Close()
			return nil, err
		}
		mp := src
		if mp.URL != "" && b.Commit != "" {
			mp.Ref = b.Commit // relative plugins come from this exact catalog
		}
		m, warns, err := ParseMarketplace(data, mp)
		if err != nil {
			b.Close()
			return nil, err
		}
		b.Marketplace, b.Warnings = m, warns
	default:
		b.Skills, b.Warnings = candidates(skills.FindAll(root, 4), nil)
		if len(b.Skills) == 0 {
			b.Close()
			return nil, fmt.Errorf("%s has no SKILL.md and is not a plugin or marketplace", src)
		}
	}
	return b, nil
}

func candidates(dirs []string, warns []string) ([]Candidate, []string) {
	var out []Candidate
	for _, d := range dirs {
		k, err := skills.ReadDir(d)
		if err != nil {
			warns = append(warns, fmt.Sprintf("%s: %v (skipped)", filepath.Base(d), err))
			continue
		}
		for _, w := range k.Warnings {
			warns = append(warns, k.Slug+": "+w)
		}
		out = append(out, Candidate{Skill: k, Dir: d, Scripts: Runnable(d)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.Slug < out[j].Skill.Slug })
	return out, warns
}

// Runnable lists files that could run: everything under scripts/ and any
// executable file.
func Runnable(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if strings.HasPrefix(rel, "scripts/") || fi.Mode()&0o111 != 0 {
			out = append(out, rel)
		}
		return nil
	})
	return out
}

// Pick chooses what to install from a bundle (nil = everything).
type Pick struct {
	Skills  []string
	Servers []string
}

// Result is what Apply installed.
type Result struct {
	Skills  []string
	Servers []string
}

// Apply writes the picked skills and servers into scope and records them.
// Everything is checked before anything is written. Unless allowScripts,
// a skill's files are installed without execute permission, so its
// scripts cannot run until the user allows them (AllowScripts).
func Apply(b *Bundle, scope Scope, pick *Pick, allowScripts bool) (Result, error) {
	var res Result
	lock, err := ReadLock(scope.LockPath)
	if err != nil {
		return res, err
	}
	want := func(list []string, name string) bool {
		if pick == nil {
			return true
		}
		for _, n := range list {
			if n == name {
				return true
			}
		}
		return false
	}
	var todo []Candidate
	for _, c := range b.Skills {
		if !want(pickSkills(pick), c.Skill.Slug) {
			continue
		}
		if !skills.ValidName(c.Skill.Slug) {
			return res, fmt.Errorf("skill folder %q is not a valid skill name", c.Skill.Slug)
		}
		todo = append(todo, c)
	}
	var servers []mcpserver.Server
	for _, s := range b.Servers {
		if want(pickServers(pick), s.Slug) {
			servers = append(servers, s)
		}
	}
	if len(todo)+len(servers) == 0 {
		return res, fmt.Errorf("nothing picked to install")
	}
	plugin := ""
	if b.Plugin != nil {
		plugin = b.Plugin.Name
	}
	for _, c := range todo {
		dst := filepath.Join(scope.SkillsDir, c.Skill.Slug)
		if rec, ok := lock.Skills[c.Skill.Slug]; exists(dst) && (!ok || rec.Source != b.Source.String()) {
			return res, fmt.Errorf("a skill named %s is already in %s from somewhere else; remove it first", c.Skill.Slug, scope.Name)
		}
	}
	for _, c := range todo {
		dst := filepath.Join(scope.SkillsDir, c.Skill.Slug)
		if err := replaceDir(c.Dir, dst, allowScripts); err != nil {
			return res, fmt.Errorf("install %s: %w", c.Skill.Slug, err)
		}
		dg, _ := Digest(dst)
		lock.Skills[c.Skill.Slug] = Record{Source: b.Source.String(), Commit: b.Commit, Digest: dg, Plugin: plugin, Scripts: allowScripts}
		res.Skills = append(res.Skills, c.Skill.Slug)
	}
	for _, s := range servers {
		if err := mcpserver.Add(scope.MCPFile, s.Slug, s.Entry()); err != nil {
			return res, err
		}
		lock.Servers[s.Slug] = Record{Source: b.Source.String(), Commit: b.Commit, Plugin: plugin}
		res.Servers = append(res.Servers, s.Slug)
	}
	return res, lock.Write(scope.LockPath)
}

func pickSkills(p *Pick) []string {
	if p == nil {
		return nil
	}
	return p.Skills
}

func pickServers(p *Pick) []string {
	if p == nil {
		return nil
	}
	return p.Servers
}

// AddServer writes one server (by URL or a pasted snippet) into scope.
func AddServer(scope Scope, s mcpserver.Server, source string) error {
	if err := mcpserver.Add(scope.MCPFile, s.Slug, s.Entry()); err != nil {
		return err
	}
	lock, err := ReadLock(scope.LockPath)
	if err != nil {
		return err
	}
	lock.Servers[s.Slug] = Record{Source: source}
	return lock.Write(scope.LockPath)
}

// ParseSnippet reads a pasted mcpServers snippet (a vendor's install
// instructions): {"mcpServers":{...}} or {"name":{...}}.
func ParseSnippet(text string) ([]mcpserver.Server, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		text = "{" + text + "}" // `"name": {...}` without braces
	}
	servers, warns, err := mcpserver.Parse([]byte(text), "snippet", "")
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		if len(warns) > 0 {
			return nil, fmt.Errorf("%s", strings.Join(warns, "; "))
		}
		return nil, fmt.Errorf("the snippet names no server")
	}
	return servers, nil
}

// AllowScripts makes an installed skill's scripts runnable (the user
// confirmed them) and records it.
func AllowScripts(scope Scope, name string) error {
	lock, err := ReadLock(scope.LockPath)
	if err != nil {
		return err
	}
	dir := filepath.Join(scope.SkillsDir, name)
	for _, rel := range Runnable(dir) {
		_ = os.Chmod(filepath.Join(dir, filepath.FromSlash(rel)), 0o755)
	}
	rec := lock.Skills[name]
	rec.Scripts = true
	lock.Skills[name] = rec
	return lock.Write(scope.LockPath)
}

// Remove deletes an installed skill or server from scope.
func Remove(scope Scope, kind, name string) error {
	lock, err := ReadLock(scope.LockPath)
	if err != nil {
		return err
	}
	switch kind {
	case "skill":
		if !skills.ValidName(name) {
			return fmt.Errorf("bad skill name %q", name)
		}
		if err := os.RemoveAll(filepath.Join(scope.SkillsDir, name)); err != nil {
			return err
		}
		delete(lock.Skills, name)
	case "server":
		if err := mcpserver.Remove(scope.MCPFile, name); err != nil {
			return err
		}
		delete(lock.Servers, name)
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	lock.SetAllBots(kind, name, false)
	return lock.Write(scope.LockPath)
}

// replaceDir copies src over dst (dst removed first); .git is skipped.
func replaceDir(src, dst string, exec bool) error {
	tmp := dst + ".installing"
	_ = os.RemoveAll(tmp)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // links could point outside the skill
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if exec && fi.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return copyFile(p, target, mode)
	})
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ServerJSON renders servers as an mcpServers document (preview).
func ServerJSON(servers []mcpserver.Server) string {
	m := map[string]json.RawMessage{}
	for _, s := range servers {
		m[s.Slug] = s.Entry()
	}
	b, _ := json.MarshalIndent(map[string]any{"mcpServers": m}, "", "  ")
	return string(b)
}

// ListFiles lists a skill's bundled files (relative), capped.
func ListFiles(dir string) ([]string, bool) { return skills.Resources(dir, 40) }
