// Package mcpbridge makes installed third-party MCP servers reachable to
// an agent under the trust model of F-034 Part C / ADR-0022:
//
//   - per-agent allowlist: a server tool is exposed only when the agent's
//     manifest names it (mcp__<server>__<tool>);
//   - the stdio child is spawned through the OS sandbox (network denied
//     unless the card declares origins);
//   - credentials come from the environment / OS keychain by NAME — the
//     card never stores a value, so secrets never touch .dhi/;
//   - every call crosses the approvals queue and the capability scopes.
//
// A server that cannot be dialed is simply not offered (the failure is
// recorded for doctor, never silently presented as a working tool).
package mcpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/credstore"
	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// ToolPrefix is the manifest tool-ref grammar for a bridged server tool.
const ToolPrefix = "mcp__"

var _ mcp.Handler = (*Bridge)(nil)

// ProtocolVersion mirrors the MCP protocol the bridge speaks.
func (b *Bridge) ProtocolVersion() string { return mcp.ProtocolVersion }

// ServerInfo names the bridged handler.
func (b *Bridge) ServerInfo() (string, string) { return "dhi-bridge", "1" }

// CallTool adapts the bridge to the mcp.Handler surface.
func (b *Bridge) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	return b.Call(ctx, name, []byte(args))
}

// Dialer opens one server. Production uses DialDefault; tests inject a
// fake caller.
type Dialer func(ctx context.Context, srv mcpserver.Server) (mcp.Caller, error)

// Lookup resolves a credential by name from the environment / keychain.
// It returns the value and whether it was found; a card lists NAMES only.
type Lookup func(name string) (string, bool)

// Deps wires the bridge for one agent turn.
type Deps struct {
	Servers   *mcpserver.Store
	Agent     *manifest.Agent
	Sandbox   sandbox.Sandbox
	Approvals *tools.Approvals
	Scopes    scopes.Set
	Dial      Dialer // nil = DialDefault
	Lookup    Lookup // nil = DefaultLookup
	// PathPrefix directories go first on a stdio server's PATH — DHI's
	// tool shims, so `uvx`/`npx` are the pinned hermetic ones (ADR-0028).
	PathPrefix []string
	// HomeDir, when set, is the stdio server's HOME and (under it) TMPDIR.
	// It must be inside the sandbox's writable roots: uvx and npx write
	// caches, logs and temp files, and under the OS sandbox they cannot
	// write the user's real ~/.cache or ~/.npm — they failed, then the
	// dial hung (ADR-0028). It also keeps third-party servers out of the
	// user's real home.
	HomeDir string
}

// Bridge is the dialed set of servers whose tools the agent allowlists.
type Bridge struct {
	deps     Deps
	clients  map[string]mcp.Caller // server slug → caller
	serverOf map[string]string     // tool name → server slug
	tools    []mcp.ToolInfo
	errs     []string
}

// New dials every server that contributes an allowlisted tool and lists
// the agent's allowed tools. Dial failures are recorded, not fatal.
func New(ctx context.Context, d Deps) *Bridge {
	b := &Bridge{deps: d, clients: map[string]mcp.Caller{}, serverOf: map[string]string{}}
	if d.Servers == nil || d.Agent == nil {
		return b
	}
	dial := d.Dial
	if dial == nil {
		dial = b.dialDefault
	}
	// Which servers does this agent allowlist at all?
	allowed := map[string]bool{}
	for _, t := range d.Agent.Tools {
		if srv, _, ok := splitTool(t); ok {
			allowed[srv] = true
		}
	}
	slugs := make([]string, 0, len(allowed))
	for s := range allowed {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		srv, ok := d.Servers.Get(slug)
		if !ok {
			b.errs = append(b.errs, fmt.Sprintf("mcp server %q not installed", slug))
			continue
		}
		caller, err := dial(ctx, srv)
		if err != nil {
			b.errs = append(b.errs, fmt.Sprintf("mcp server %q: %v", slug, err))
			continue
		}
		remote, err := caller.Tools(ctx)
		if err != nil {
			_ = caller.Close()
			b.errs = append(b.errs, fmt.Sprintf("mcp server %q: list tools: %v", slug, err))
			continue
		}
		b.clients[slug] = caller
		for _, rt := range remote {
			name := ToolPrefix + slug + "__" + rt.Name
			if !agentAllows(d.Agent, name) {
				continue
			}
			b.tools = append(b.tools, mcp.ToolInfo{
				Name: name, Description: rt.Description, InputSchema: rt.InputSchema,
			})
			b.serverOf[name] = slug
		}
	}
	sort.Slice(b.tools, func(i, j int) bool { return b.tools[i].Name < b.tools[j].Name })
	return b
}

// Tools lists the bridged tools the agent may call.
func (b *Bridge) Tools() []mcp.ToolInfo { return append([]mcp.ToolInfo(nil), b.tools...) }

// Errors names servers/tools that could not be offered (doctor / audit).
func (b *Bridge) Errors() []string { return append([]string(nil), b.errs...) }

// Close tears down every dialed client.
func (b *Bridge) Close() {
	for _, c := range b.clients {
		_ = c.Close()
	}
	b.clients = map[string]mcp.Caller{}
}

// Call routes one bridged tool call: the agent must allowlist it, the
// network scope must permit it, and the human must approve it before the
// server is reached.
func (b *Bridge) Call(ctx context.Context, name string, args []byte) (string, bool, error) {
	slug, remote, ok := splitTool(name)
	if !ok {
		return "unknown bridged tool " + name, true, nil
	}
	if !agentAllows(b.deps.Agent, name) {
		return "tool " + name + " is not in " + agentID(b.deps.Agent) + "'s allowlist", true, nil
	}
	caller, ok := b.clients[slug]
	if !ok {
		return "mcp server " + slug + " is not available (not installed or failed to dial)", true, nil
	}
	// Network scope gates reaching a third-party server (deny-by-default).
	set := b.deps.Scopes
	if set == nil {
		set = scopes.Default()
	}
	sc := scopes.ToolScope(name)
	switch set.EffectFor(sc) {
	case scopes.Deny:
		return "tool " + name + " denied by capability scope (" + string(sc) + ")", true, nil
	case scopes.Ask:
		if b.deps.Approvals == nil {
			return "tool " + name + " unavailable: approvals queue not configured", true, nil
		}
		if err := b.deps.Approvals.AskScope(ctx, agentID(b.deps.Agent), string(sc),
			sandbox.OpExec, "tool "+name, "agent-requested MCP tool ("+slug+")"); err != nil {
			return err.Error(), true, nil
		}
	}
	out, isErr, err := caller.CallTool(ctx, remote, args)
	if err != nil {
		return "", true, err
	}
	return out, isErr, nil
}

// dialDefault opens a production server: stdio under the sandbox, http
// restricted to a declared origin.
func (b *Bridge) dialDefault(ctx context.Context, srv mcpserver.Server) (mcp.Caller, error) {
	switch srv.Transport {
	case mcpserver.Stdio:
		argv, err := b.stdioArgv(srv)
		if err != nil {
			return nil, err
		}
		env, err := b.stdioEnv(srv)
		if err != nil {
			return nil, err
		}
		return mcp.DialStdio(ctx, env, argv...)
	case mcpserver.HTTP:
		u, err := url.Parse(srv.URL)
		if err != nil {
			return nil, fmt.Errorf("bad url: %w", err)
		}
		if !originDeclared(srv, u.Host) {
			return nil, fmt.Errorf("url host %q is not a declared origin", u.Host)
		}
		var client *http.Client
		if srv.AuthEnv != "" {
			lookup := b.deps.Lookup
			if lookup == nil {
				lookup = DefaultLookup
			}
			token, ok := lookup(srv.AuthEnv)
			if !ok {
				return nil, fmt.Errorf("credential %s is not set (add it in the setup wizard, or export it)", srv.AuthEnv)
			}
			client = mcp.BearerClient(token, nil)
		}
		return mcp.DialHTTP(ctx, srv.URL, client)
	default:
		return nil, fmt.Errorf("unknown transport %q", srv.Transport)
	}
}

// stdioArgv wraps the server command in the OS sandbox. Network is
// allowed only when the card declares origins (best-effort containment —
// the OS adapters are boolean; declared origins are audited).
func (b *Bridge) stdioArgv(srv mcpserver.Server) ([]string, error) {
	argv := append([]string{srv.Command}, srv.Args...)
	if b.deps.Sandbox == nil || b.deps.Sandbox.Name() == "noop" {
		return argv, nil
	}
	if len(srv.Origins) > 0 {
		if np, ok := b.deps.Sandbox.(sandbox.NetworkPolicy); ok {
			wrapped, err := np.WrapNetwork(argv, true)
			if err != nil {
				return nil, err
			}
			return wrapped, nil
		}
		return argv, nil
	}
	return b.deps.Sandbox.Wrap(argv)
}

// stdioEnv builds the child env from PATH + the card's declared names,
// resolved through the keychain/env lookup. A declared-but-missing
// credential is a named refusal (never an empty value).
func (b *Bridge) stdioEnv(srv mcpserver.Server) ([]string, error) {
	env := []string{}
	path := os.Getenv("PATH")
	if len(b.deps.PathPrefix) > 0 {
		pre := strings.Join(b.deps.PathPrefix, string(os.PathListSeparator))
		if path == "" {
			path = pre
		} else {
			path = pre + string(os.PathListSeparator) + path
		}
	}
	if path != "" {
		env = append(env, "PATH="+path)
	}
	lookup := b.deps.Lookup
	if lookup == nil {
		lookup = DefaultLookup
	}
	if home := b.deps.HomeDir; home != "" {
		tmp := filepath.Join(home, "tmp")
		if err := os.MkdirAll(tmp, 0o700); err != nil {
			return nil, fmt.Errorf("mcp home %s: %w", home, err)
		}
		env = append(env, "HOME="+home, "TMPDIR="+tmp)
	}
	for _, name := range srv.Env {
		val, ok := lookup(name)
		if !ok {
			return nil, fmt.Errorf("credential %s is not set (declare it in the OS keychain or environment)", name)
		}
		env = append(env, name+"="+val)
	}
	return env, nil
}

// DialDefault is the production dialer (exposed for runtime wiring and
// tests that want the real argv/env assembly with a fake transport).
func DialDefault(ctx context.Context, srv mcpserver.Server, sb sandbox.Sandbox, lookup Lookup) (mcp.Caller, error) {
	b := &Bridge{deps: Deps{Sandbox: sb, Lookup: lookup}}
	return b.dialDefault(ctx, srv)
}

// DialWith is DialDefault with the full production Deps (shim PATH, private
// HOME, credential lookup) — what the runtime's bridge uses per server.
func DialWith(ctx context.Context, srv mcpserver.Server, d Deps) (mcp.Caller, error) {
	b := &Bridge{deps: d}
	return b.dialDefault(ctx, srv)
}

// DefaultLookup resolves a credential from the environment, then the
// user-level credentials file, then (on macOS) the login keychain. Values
// never come from .dhi/ (ADR-0028).
func DefaultLookup(name string) (string, bool) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v, true
	}
	if st, err := credstore.Open(); err == nil {
		if v, ok := st.Lookup(name); ok {
			return v, true
		}
	}
	if runtime.GOOS == "darwin" {
		if v, ok := keychainLookup(name); ok {
			return v, true
		}
	}
	return "", false
}

func keychainLookup(name string) (string, bool) {
	path, err := exec.LookPath("security")
	if err != nil {
		return "", false
	}
	out, err := exec.Command(path, "find-generic-password", "-s", "dhi-mcp", "-a", name, "-w").Output()
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(out))
	return v, v != ""
}

// splitTool parses mcp__<server>__<tool>.
func splitTool(name string) (server, tool string, ok bool) {
	if !strings.HasPrefix(name, ToolPrefix) {
		return "", "", false
	}
	rest := name[len(ToolPrefix):]
	i := strings.Index(rest, "__")
	if i <= 0 || i+2 >= len(rest) {
		return "", "", false
	}
	return rest[:i], rest[i+2:], true
}

func agentAllows(a *manifest.Agent, name string) bool {
	if a == nil {
		return false
	}
	for _, t := range a.Tools {
		if t == name {
			return true
		}
		// mcp__<server>__* allowlists every tool of that server.
		if strings.HasSuffix(t, "__*") && strings.HasPrefix(name, strings.TrimSuffix(t, "*")) {
			return true
		}
	}
	return false
}

func agentID(a *manifest.Agent) string {
	if a == nil {
		return "agent"
	}
	return a.ID
}

func originDeclared(srv mcpserver.Server, host string) bool {
	host = strings.ToLower(host)
	for _, o := range srv.Origins {
		if strings.EqualFold(o, host) || strings.HasPrefix(host, strings.ToLower(o)+":") {
			return true
		}
	}
	return false
}
