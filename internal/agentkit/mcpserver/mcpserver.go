// Package mcpserver reads and writes MCP server configuration in the
// standard "mcpServers" JSON (ADR-0043) — the same shape Claude Code's
// .mcp.json, Cursor, VS Code and the vendors' own plugins use:
//
//	{"mcpServers": {
//	  "atlassian": {"type": "http", "url": "https://mcp.atlassian.com/v2/mcp"},
//	  "slack": {"type": "http", "url": "https://mcp.slack.com/mcp",
//	            "oauth": {"clientId": "…", "callbackPort": 3118}},
//	  "zoom-mcp": {"type": "http", "url": "https://mcp.zoom.us/mcp/zoom/streamable",
//	               "headers": {"Authorization": "Bearer ${ZOOM_MCP_ACCESS_TOKEN}"}},
//	  "files": {"command": "npx", "args": ["-y", "@scope/server"], "env": {"TOKEN": "${TOKEN}"}}
//	}}
//
// Values may reference variables as ${NAME} (or ${NAME:-default}); DHI
// resolves them at dial time from the environment and the secret store,
// so a config file never needs to hold a credential.
//
// Servers come from, in rising precedence: your own file
// (<config>/dhi/mcp.json, every workspace), a member repo's .mcp.json
// (read only — the repo's own config for every tool), and the
// workspace's .dhi/mcp.json. Malformed entries are warnings, never fatal
// (ADR-0011).
package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// File is the workspace's server file, relative to its root.
const File = ".dhi/mcp.json"

// RepoFile is a repository's own MCP config (read only).
const RepoFile = ".mcp.json"

// UserFile is your servers for every workspace: <config>/dhi/mcp.json.
// Set once by the app at startup; "" (tests, tools) means none.
var UserFile string

// Transport is how DHI reaches a server.
type Transport string

// Transports. stdio spawns a child under the sandbox; http dials a URL
// (MCP streamable HTTP).
const (
	Stdio Transport = "stdio"
	HTTP  Transport = "http"
)

// Sources: where a server's entry was read from.
const (
	SourceWorkspace = "workspace"
	SourceUser      = "yours"
	SourceRepo      = "repo"
)

// OAuth is a server's pre-registered OAuth client, as a vendor's plugin
// publishes it (Slack's). Servers without one use dynamic registration.
type OAuth struct {
	ClientID     string `json:"clientId,omitempty"`
	CallbackPort int    `json:"callbackPort,omitempty"`
}

// Server is one configured MCP server.
type Server struct {
	Slug      string
	Transport Transport
	Command   string            // stdio
	Args      []string          // stdio
	Env       map[string]string // stdio: values may hold ${VAR}
	URL       string            // http
	Headers   map[string]string // http: values may hold ${VAR}
	OAuth     *OAuth
	Source    string // SourceWorkspace, SourceUser, SourceRepo
	File      string // the file the entry came from
}

// entry is one server's JSON object.
type entry struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	OAuth   *OAuth            `json:"oauth,omitempty"`
}

var slugRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidSlug reports whether name can name a server (it becomes part of
// the agent tool names mcp__<name>__<tool>).
func ValidSlug(name string) bool { return slugRe.MatchString(name) && !strings.Contains(name, "__") }

// varRe matches ${NAME} and ${NAME:-default}.
var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// Vars lists the variables the server's config references, sorted: the
// names DHI asks for (masked) when they are not set.
func (s Server) Vars() []string {
	seen := map[string]bool{}
	scan := func(v string) {
		for _, m := range varRe.FindAllStringSubmatch(v, -1) {
			if m[2] == "" { // a default means it is not required
				seen[m[1]] = true
			}
		}
	}
	scan(s.URL)
	scan(s.Command)
	for _, a := range s.Args {
		scan(a)
	}
	for _, v := range s.Env {
		scan(v)
	}
	for _, v := range s.Headers {
		scan(v)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Expand replaces ${NAME} and ${NAME:-default} in v through lookup; a
// required variable that is not set is a named error.
func Expand(v string, lookup func(string) (string, bool)) (string, error) {
	var missing []string
	out := varRe.ReplaceAllStringFunc(v, func(m string) string {
		parts := varRe.FindStringSubmatch(m)
		if val, ok := lookup(parts[1]); ok {
			return val
		}
		if strings.Contains(m, ":-") {
			return parts[2]
		}
		missing = append(missing, parts[1])
		return ""
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%s is not set", strings.Join(missing, ", "))
	}
	return out, nil
}

// Host is an http server's host[:port] ("" for stdio).
func (s Server) Host() string {
	u, err := url.Parse(s.URL)
	if err != nil {
		return ""
	}
	return u.Host
}

// Parse reads a server file: {"mcpServers": {...}} (Claude Code, Cursor,
// a plugin's .mcp.json) or, as some vendors publish snippets, the bare
// {"name": {...}} map. Invalid entries are skipped with a warning.
func Parse(data []byte, source, file string) ([]Server, []string, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("mcp: %s is not valid JSON: %w", displayFile(file), err)
	}
	servers := doc
	if raw, ok := doc["mcpServers"]; ok {
		servers = nil
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, nil, fmt.Errorf("mcp: %s: mcpServers is not an object: %w", displayFile(file), err)
		}
	}
	var out []Server
	var warns []string
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		s, err := ParseEntry(name, servers[name])
		if err != nil {
			warns = append(warns, fmt.Sprintf("mcp %s in %s: %v (skipped)", name, displayFile(file), err))
			continue
		}
		s.Source, s.File = source, file
		out = append(out, s)
	}
	return out, warns, nil
}

// ParseEntry validates one server's JSON object.
func ParseEntry(name string, raw json.RawMessage) (Server, error) {
	if !ValidSlug(name) {
		return Server{}, fmt.Errorf("name %q must be letters, digits, '.', '_' or '-'", name)
	}
	var e entry
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&e); err != nil {
		return Server{}, err
	}
	s := Server{Slug: name, Command: strings.TrimSpace(e.Command), Args: e.Args, Env: e.Env,
		URL: strings.TrimSpace(e.URL), Headers: e.Headers, OAuth: e.OAuth}
	switch strings.ToLower(e.Type) {
	case "", "stdio":
		if s.Command == "" && s.URL != "" && e.Type == "" {
			s.Transport = HTTP
		} else {
			s.Transport = Stdio
		}
	case "http", "streamable-http", "streamable_http", "streamablehttp":
		s.Transport = HTTP
	case "sse":
		return Server{}, fmt.Errorf("the legacy SSE transport is not supported; use the server's streamable HTTP URL")
	default:
		return Server{}, fmt.Errorf("unknown type %q (want stdio or http)", e.Type)
	}
	switch s.Transport {
	case Stdio:
		if s.Command == "" {
			return Server{}, fmt.Errorf("a stdio server needs a command")
		}
	case HTTP:
		if s.URL == "" {
			return Server{}, fmt.Errorf("an http server needs a url")
		}
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" {
			return Server{}, fmt.Errorf("bad url %q", s.URL)
		}
		if u.Scheme != "https" && !isLoopback(u.Hostname()) {
			return Server{}, fmt.Errorf("url must be https (http only for a loopback test server)")
		}
	}
	return s, nil
}

func isLoopback(host string) bool { return host == "127.0.0.1" || host == "localhost" || host == "::1" }

// Entry is the JSON object for s, as written to a server file.
func (s Server) Entry() json.RawMessage {
	e := entry{Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL, Headers: s.Headers, OAuth: s.OAuth}
	if s.Transport == HTTP {
		e.Type = "http"
	}
	b, _ := json.Marshal(e)
	return b
}

// Store is the merged server set for one workspace.
type Store struct {
	items    map[string]Server
	warnings []string
}

// Open loads every server for the workspace at root ("" = only yours).
func Open(root string) *Store {
	return OpenWith(root, nil)
}

// OpenWith is Open that also reads each member repo's .mcp.json.
func OpenWith(root string, members []workspace.Member) *Store {
	s := &Store{items: map[string]Server{}}
	s.load(UserFile, SourceUser)
	for _, m := range members {
		s.load(filepath.Join(m.Path, RepoFile), SourceRepo)
	}
	if root != "" {
		s.load(filepath.Join(root, File), SourceWorkspace)
	}
	return s
}

func (s *Store) load(path, source string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("mcp: read %s: %v", path, err))
		return
	}
	servers, warns, err := Parse(data, source, path)
	if err != nil {
		s.warnings = append(s.warnings, err.Error())
		return
	}
	s.warnings = append(s.warnings, warns...)
	for _, srv := range servers {
		if prev, ok := s.items[srv.Slug]; ok && prev.Source != srv.Source {
			s.warnings = append(s.warnings, fmt.Sprintf("mcp %s: the %s entry replaces the %s one", srv.Slug, source, prev.Source))
		}
		s.items[srv.Slug] = srv
	}
}

// Servers lists servers sorted by name.
func (s *Store) Servers() []Server {
	out := make([]Server, 0, len(s.items))
	for _, v := range s.items {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// Get fetches one server.
func (s *Store) Get(slug string) (Server, bool) {
	srv, ok := s.items[slug]
	return srv, ok
}

// Warnings names unreadable files and skipped entries.
func (s *Store) Warnings() []string { return append([]string(nil), s.warnings...) }

// Add writes (or replaces) server name in the file at path, keeping
// every other entry and key exactly as it was.
func Add(path, name string, e json.RawMessage) error {
	if _, err := ParseEntry(name, e); err != nil {
		return fmt.Errorf("mcp %s: %w", name, err)
	}
	return edit(path, func(servers map[string]json.RawMessage) { servers[name] = e })
}

// Remove deletes server name from the file at path (absent is fine).
func Remove(path, name string) error {
	return edit(path, func(servers map[string]json.RawMessage) { delete(servers, name) })
}

func edit(path string, change func(map[string]json.RawMessage)) error {
	doc := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("mcp: %s is not valid JSON (fix or remove it): %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := doc["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("mcp: %s: mcpServers is not an object", path)
		}
	}
	change(servers)
	enc, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	doc["mcpServers"] = enc
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	// MarshalIndent of RawMessage keeps the inner bytes compact; indent
	// the whole document once more for a readable file.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out, "", "  "); err != nil {
		return err
	}
	pretty.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pretty.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WorkspaceFile is the workspace's server file.
func WorkspaceFile(root string) string { return filepath.Join(root, File) }

func displayFile(p string) string {
	if p == "" {
		return "the snippet"
	}
	return p
}

// HTTPServer builds the entry for a remote server by URL (the "add by
// URL" path): DHI logs in on first use.
func HTTPServer(name, rawURL string) (Server, error) {
	s, err := ParseEntry(name, json.RawMessage(`{"type":"http","url":`+strconv.Quote(rawURL)+`}`))
	return s, err
}
