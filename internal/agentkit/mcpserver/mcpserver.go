// Package mcpserver models installed third-party MCP servers (F-034,
// ADR-0022). One strict TOML card per server lives under .dhi/mcp/<slug>.toml.
// A card declares *how* to reach the server — never a credential value:
// `env` lists the variable names whose values are resolved from the OS
// keychain / environment at spawn time, so secrets never touch .dhi/.
// Network access is a declared origin list; anything else is denied by
// the sandbox. Packs ship these cards; the runtime bridges them into the
// served tool surface under the agent's scope. A card a pack installs is
// provenance-tracked and removed exactly on uninstall.
package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// SchemaVersion is the server-card schema this build understands.
const SchemaVersion = 1

// Dir is the reserved tree holding server cards.
const Dir = workspace.DirMCP

var (
	slugRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	originRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*(\:[0-9]{1,5})?$`)
)

// Transport is how DHI reaches a server.
type Transport string

// Transports. stdio spawns a child under the sandbox; http dials a URL.
const (
	Stdio Transport = "stdio"
	HTTP  Transport = "http"
)

// Server is one installed MCP server card.
type Server struct {
	Slug        string
	Name        string
	Description string
	Transport   Transport
	Command     string   // stdio: executable
	Args        []string // stdio: arguments
	URL         string   // http: endpoint
	Env         []string // declared env var NAMES (values resolved at spawn)
	Origins     []string // declared network origins (host[:port])
}

type file struct {
	Schema      int      `toml:"schema"`
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Transport   string   `toml:"transport"`
	Command     string   `toml:"command"`
	Args        []string `toml:"args"`
	URL         string   `toml:"url"`
	Env         []string `toml:"env"`
	Origins     []string `toml:"origins"`
}

// Parse decodes and validates one server card strictly.
func Parse(slug string, data []byte) (*Server, error) {
	if !slugRe.MatchString(slug) {
		return nil, fmt.Errorf("mcp: bad slug %q (lowercase [a-z0-9._-])", slug)
	}
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, fmt.Errorf("mcp: parse %s: %w", slug, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("mcp: %s: unknown key(s): %s (bump schema?)", slug, strings.Join(keys, ", "))
	}
	if f.Schema != SchemaVersion {
		return nil, fmt.Errorf("mcp: %s: schema %d, want %d", slug, f.Schema, SchemaVersion)
	}
	s := &Server{
		Slug:        slug,
		Name:        strings.TrimSpace(f.Name),
		Description: strings.TrimSpace(f.Description),
		Transport:   Transport(f.Transport),
		Command:     strings.TrimSpace(f.Command),
		Args:        f.Args,
		URL:         strings.TrimSpace(f.URL),
		Env:         cleanStrings(f.Env),
		Origins:     cleanStrings(f.Origins),
	}
	if s.Name == "" {
		return nil, fmt.Errorf("mcp: %s: name is required", slug)
	}
	switch s.Transport {
	case Stdio:
		if s.Command == "" {
			return nil, fmt.Errorf("mcp: %s: stdio server needs a command", slug)
		}
		if s.URL != "" {
			return nil, fmt.Errorf("mcp: %s: stdio server must not set url", slug)
		}
	case HTTP:
		if s.URL == "" {
			return nil, fmt.Errorf("mcp: %s: http server needs a url", slug)
		}
		if s.Command != "" {
			return nil, fmt.Errorf("mcp: %s: http server must not set command", slug)
		}
		if !strings.HasPrefix(s.URL, "https://") && !isLoopbackHTTP(s.URL) {
			return nil, fmt.Errorf("mcp: %s: url must be https (http only for loopback tests)", slug)
		}
	default:
		return nil, fmt.Errorf("mcp: %s: bad transport %q (want stdio or http)", slug, f.Transport)
	}
	for _, e := range s.Env {
		if !envNameRe.MatchString(e) {
			return nil, fmt.Errorf("mcp: %s: env %q is not a variable name (declare names, never values)", slug, e)
		}
	}
	for _, o := range s.Origins {
		if !originRe.MatchString(o) {
			return nil, fmt.Errorf("mcp: %s: bad origin %q (want host[:port])", slug, o)
		}
	}
	return s, nil
}

// Write validates-then-writes a card atomically under root/.dhi/mcp.
func Write(root string, s *Server) error {
	var f file
	f.Schema = SchemaVersion
	f.Name = s.Name
	f.Description = s.Description
	f.Transport = string(s.Transport)
	f.Command = s.Command
	f.Args = s.Args
	f.URL = s.URL
	f.Env = cleanStrings(s.Env)
	f.Origins = cleanStrings(s.Origins)
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(f); err != nil {
		return fmt.Errorf("mcp: encode: %w", err)
	}
	if _, err := Parse(s.Slug, []byte(buf.String())); err != nil {
		return fmt.Errorf("mcp: self-check: %w", err)
	}
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mcp: mkdir: %w", err)
	}
	return atomicWrite(Path(root, s.Slug), buf.String())
}

// Delete removes a server card; a missing card is not an error (uninstall
// tolerates manual removal).
func Delete(root, slug string) error {
	if err := os.Remove(Path(root, slug)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("mcp: delete %s: %w", slug, err)
	}
	return nil
}

// Path is the card location for a slug.
func Path(root, slug string) string {
	return filepath.Join(root, Dir, slug+".toml")
}

// Store is the loaded server set.
type Store struct {
	items    map[string]Server
	order    []string
	warnings []string
}

// Open loads every card under root/.dhi/mcp. A missing dir is empty;
// malformed cards are warnings, never fatal (ADR-0011).
func Open(root string) *Store {
	s := &Store{items: map[string]Server{}}
	entries, err := os.ReadDir(filepath.Join(root, Dir))
	if os.IsNotExist(err) {
		return s
	}
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("mcp: read %s: %v", Dir, err))
		return s
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".toml")
		data, err := os.ReadFile(filepath.Join(root, Dir, e.Name()))
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("mcp %s: %v", slug, err))
			continue
		}
		srv, err := Parse(slug, data)
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("mcp %s: %v", slug, err))
			continue
		}
		s.items[slug] = *srv
		s.order = append(s.order, slug)
	}
	sort.Strings(s.order)
	return s
}

// Servers lists servers sorted by slug.
func (s *Store) Servers() []Server {
	out := make([]Server, 0, len(s.order))
	for _, slug := range s.order {
		out = append(out, s.items[slug])
	}
	return out
}

// Get fetches one server.
func (s *Store) Get(slug string) (Server, bool) {
	srv, ok := s.items[slug]
	return srv, ok
}

// Warnings names malformed cards.
func (s *Store) Warnings() []string { return append([]string(nil), s.warnings...) }

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func isLoopbackHTTP(u string) bool {
	return strings.HasPrefix(u, "http://127.0.0.1:") || strings.HasPrefix(u, "http://localhost:")
}

func atomicWrite(path, data string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
