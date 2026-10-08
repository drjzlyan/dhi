package mcpbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpserver"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/credstore"
	"github.com/drjzlyan/dhi/internal/mcp"
)

type fakeCaller struct {
	tools  []mcp.ToolInfo
	last   string
	closed bool
}

func (f *fakeCaller) Tools(context.Context) ([]mcp.ToolInfo, error) { return f.tools, nil }
func (f *fakeCaller) CallTool(_ context.Context, name string, _ json.RawMessage) (string, bool, error) {
	f.last = name
	return "called " + name, false, nil
}
func (f *fakeCaller) Close() error { f.closed = true; return nil }

// openServers writes one server card and loads the store.
func openServers(t *testing.T, srv mcpserver.Server) *mcpserver.Store {
	t.Helper()
	root := t.TempDir()
	if err := mcpserver.Write(root, &srv); err != nil {
		t.Fatal(err)
	}
	return mcpserver.Open(root)
}

func fakeDialer(c *fakeCaller) Dialer {
	return func(context.Context, mcpserver.Server) (mcp.Caller, error) { return c, nil }
}

func TestBridgeAllowlistAndRouting(t *testing.T) {
	srv := mcpserver.Server{Slug: "fs", Name: "FS", Transport: mcpserver.Stdio, Command: "npx"}
	servers := openServers(t, srv)
	caller := &fakeCaller{tools: []mcp.ToolInfo{
		{Name: "read", Description: "read a file"},
		{Name: "write", Description: "write a file"},
	}}
	agent := &manifest.Agent{ID: "scout", Tools: []string{"mcp__fs__read"}}
	b := New(context.Background(), Deps{
		Servers: servers, Agent: agent, Dial: fakeDialer(caller),
		Scopes: scopes.Set{scopes.Network: scopes.Auto},
	})
	defer b.Close()

	got := b.Tools()
	if len(got) != 1 || got[0].Name != "mcp__fs__read" {
		t.Fatalf("tools = %+v", got)
	}
	// Calling an allowed tool routes to the server.
	out, isErr, err := b.Call(context.Background(), "mcp__fs__read", []byte(`{}`))
	if err != nil || isErr || out != "called read" || caller.last != "read" {
		t.Fatalf("call = %q isErr=%v err=%v last=%q", out, isErr, err, caller.last)
	}
	// A tool not in the allowlist is refused, not routed.
	if out, isErr, _ := b.Call(context.Background(), "mcp__fs__write", []byte(`{}`)); !isErr || !strings.Contains(out, "allowlist") {
		t.Fatalf("un-allowlisted call = %q isErr=%v", out, isErr)
	}
	// Calling a tool for a server that was not allowlisted refuses.
	if _, isErr, _ := b.Call(context.Background(), "mcp__ghost__read", []byte(`{}`)); !isErr {
		t.Fatal("unknown server tool accepted")
	}
}

func TestBridgeScopeAndApprovals(t *testing.T) {
	srv := mcpserver.Server{Slug: "fs", Name: "FS", Transport: mcpserver.Stdio, Command: "npx"}
	agent := &manifest.Agent{ID: "scout", Tools: []string{"mcp__fs__read"}}
	caller := &fakeCaller{tools: []mcp.ToolInfo{{Name: "read"}}}

	// Network denied by default → refused.
	b := New(context.Background(), Deps{
		Servers: openServers(t, srv), Agent: agent, Dial: fakeDialer(caller),
		Scopes: scopes.Set{scopes.Network: scopes.Deny},
	})
	if out, isErr, _ := b.Call(context.Background(), "mcp__fs__read", nil); !isErr || !strings.Contains(out, "denied") {
		t.Fatalf("deny = %q isErr=%v", out, isErr)
	}
	b.Close()

	// Network ask → parks on the approvals queue; denying refuses.
	apr := tools.NewApprovals()
	b = New(context.Background(), Deps{
		Servers: openServers(t, srv), Agent: agent, Dial: fakeDialer(caller),
		Scopes: scopes.Set{scopes.Network: scopes.Ask}, Approvals: apr,
	})
	done := make(chan bool, 1)
	go func() {
		_, isErr, _ := b.Call(context.Background(), "mcp__fs__read", nil)
		done <- isErr
	}()
	for len(apr.List()) == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	apr.Resolve(apr.List()[0].ID, false)
	if isErr := <-done; !isErr {
		t.Fatal("denied approval still called the server")
	}
	b.Close()

	// Network ask + allow → proceeds.
	apr2 := tools.NewApprovals()
	b = New(context.Background(), Deps{
		Servers: openServers(t, srv), Agent: agent, Dial: fakeDialer(caller),
		Scopes: scopes.Set{scopes.Network: scopes.Ask}, Approvals: apr2,
	})
	done2 := make(chan bool, 1)
	go func() {
		_, isErr, _ := b.Call(context.Background(), "mcp__fs__read", nil)
		done2 <- isErr
	}()
	for len(apr2.List()) == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	apr2.Resolve(apr2.List()[0].ID, true)
	if isErr := <-done2; isErr {
		t.Fatal("allowed approval was still refused")
	}
	b.Close()
}

func TestBridgeRecordsUndialableServer(t *testing.T) {
	srv := mcpserver.Server{Slug: "fs", Name: "FS", Transport: mcpserver.Stdio, Command: "npx"}
	agent := &manifest.Agent{ID: "scout", Tools: []string{"mcp__fs__read"}}
	b := New(context.Background(), Deps{
		Servers: openServers(t, srv), Agent: agent,
		Dial: func(context.Context, mcpserver.Server) (mcp.Caller, error) {
			return nil, os.ErrNotExist
		},
	})
	if len(b.Tools()) != 0 {
		t.Fatalf("undialable server offered tools: %+v", b.Tools())
	}
	if len(b.Errors()) != 1 || !strings.Contains(b.Errors()[0], "fs") {
		t.Fatalf("errors = %v", b.Errors())
	}
	// A tool from an undialed server refuses by name.
	if out, isErr, _ := b.Call(context.Background(), "mcp__fs__read", nil); !isErr || !strings.Contains(out, "not available") {
		t.Fatalf("call = %q isErr=%v", out, isErr)
	}
}

// recSandbox records wrap calls.
type recSandbox struct {
	wraps    int
	netCalls []bool
}

func (r *recSandbox) Name() string { return "rec" }
func (r *recSandbox) Wrap(argv []string) ([]string, error) {
	r.wraps++
	return append([]string{"WRAP", "--"}, argv...), nil
}
func (r *recSandbox) WrapNetwork(argv []string, allow bool) ([]string, error) {
	r.netCalls = append(r.netCalls, allow)
	return append([]string{"WRAPNET", "--"}, argv...), nil
}

func TestStdioSandboxAndEnv(t *testing.T) {
	sb := &recSandbox{}
	b := &Bridge{deps: Deps{Sandbox: sb, Lookup: func(name string) (string, bool) {
		if name == "FS_TOKEN" {
			return "s3cret", true
		}
		return "", false
	}}}
	// No declared origins → sandbox.Wrap (network denied by profile).
	argv, err := b.stdioArgv(mcpserver.Server{Command: "npx", Args: []string{"srv"}})
	if err != nil || sb.wraps != 1 || argv[0] != "WRAP" {
		t.Fatalf("argv = %v wraps=%d err=%v", argv, sb.wraps, err)
	}
	// Declared origins → WrapNetwork(allow=true).
	argv, err = b.stdioArgv(mcpserver.Server{Command: "npx", Origins: []string{"api.example.com"}})
	if err != nil || len(sb.netCalls) != 1 || sb.netCalls[0] != true || argv[0] != "WRAPNET" {
		t.Fatalf("network argv = %v net=%v err=%v", argv, sb.netCalls, err)
	}
	// Credentials resolve by name; a missing one is a named refusal.
	env, err := b.stdioEnv(mcpserver.Server{Env: []string{"FS_TOKEN"}})
	if err != nil || !contains(env, "FS_TOKEN=s3cret") {
		t.Fatalf("env = %v err=%v", env, err)
	}
	if _, err := b.stdioEnv(mcpserver.Server{Env: []string{"MISSING"}}); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("missing credential = %v", err)
	}
}

func TestHTTPOriginEnforced(t *testing.T) {
	servers := []mcpserver.Server{
		{Slug: "web", Name: "Web", Transport: mcpserver.HTTP, URL: "https://api.example.com/mcp", Origins: []string{"api.example.com"}},
	}
	// Declared origin passes the origin gate (dial then fails on network,
	// which is fine — the gate is what we assert).
	b := &Bridge{deps: Deps{}}
	b.deps.Sandbox = nil
	if _, err := b.dialDefault(context.Background(), servers[0]); err != nil && strings.Contains(err.Error(), "not a declared origin") {
		t.Fatalf("declared origin refused: %v", err)
	}
	bad := mcpserver.Server{Slug: "web", Name: "Web", Transport: mcpserver.HTTP, URL: "https://evil.example.com/mcp", Origins: []string{"api.example.com"}}
	if _, err := b.dialDefault(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "not a declared origin") {
		t.Fatalf("undeclared origin = %v", err)
	}
}

func TestDefaultLookupEnv(t *testing.T) {
	t.Setenv("DHI_MCP_TEST_TOKEN", "abc")
	if v, ok := DefaultLookup("DHI_MCP_TEST_TOKEN"); !ok || v != "abc" {
		t.Fatalf("lookup = %q ok=%v", v, ok)
	}
	if _, ok := DefaultLookup("DHI_MCP_TEST_ABSENT"); ok {
		t.Fatal("absent credential reported found")
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---- ADR-0028: bearer auth, shim PATH, wildcard, credential file ----

func TestHTTPServerGetsItsBearerCredential(t *testing.T) {
	var got string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var in struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		json.Unmarshal(body, &in)
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(in.ID) + `,"result":{}}`))
	}))
	defer hs.Close()
	u, _ := url.Parse(hs.URL)
	srv := mcpserver.Server{Slug: "linear", Name: "L", Transport: mcpserver.HTTP, URL: hs.URL,
		Origins: []string{u.Host}, AuthEnv: "LINEAR_API_KEY"}

	b := &Bridge{deps: Deps{Lookup: func(n string) (string, bool) {
		return "key-xyz", n == "LINEAR_API_KEY"
	}}}
	c, err := b.dialDefault(context.Background(), srv)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if got != "Bearer key-xyz" {
		t.Fatalf("Authorization = %q", got)
	}

	missing := &Bridge{deps: Deps{Lookup: func(string) (string, bool) { return "", false }}}
	if _, err := missing.dialDefault(context.Background(), srv); err == nil ||
		!strings.Contains(err.Error(), "credential LINEAR_API_KEY is not set") {
		t.Fatalf("missing credential = %v; it must refuse by name", err)
	}
}

func TestStdioPathStartsWithTheToolShims(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	b := &Bridge{deps: Deps{PathPrefix: []string{"/dhi/bin", "/dhi/other"}}}
	env, err := b.stdioEnv(mcpserver.Server{})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 1 || env[0] != "PATH=/dhi/bin:/dhi/other:/usr/bin:/bin" {
		t.Fatalf("env = %v", env)
	}
	plain := &Bridge{}
	env, _ = plain.stdioEnv(mcpserver.Server{})
	if len(env) != 1 || env[0] != "PATH=/usr/bin:/bin" {
		t.Fatalf("no prefix must leave PATH alone: %v", env)
	}
}

func TestWildcardToolRefAllowsOnlyThatServer(t *testing.T) {
	a := &manifest.Agent{ID: "a", Tools: []string{"mcp__jira__*", "mcp__slack__post"}}
	for name, want := range map[string]bool{
		"mcp__jira__search": true, "mcp__jira__create_issue": true,
		"mcp__slack__post": true, "mcp__slack__delete": false,
		"mcp__jiraextra__x": false, "mcp__other__x": false,
	} {
		if got := agentAllows(a, name); got != want {
			t.Errorf("agentAllows(%s) = %v, want %v", name, got, want)
		}
	}
	if !manifest.ValidToolRef("mcp__jira__*") || manifest.ValidToolRef("mcp__jira__") || manifest.ValidToolRef("mcp__*") {
		t.Fatal("wildcard grammar wrong")
	}
}

func TestDefaultLookupReadsTheCredentialFileAfterTheEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := credstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(map[string]string{"DHI_TEST_CRED": "from-file"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := DefaultLookup("DHI_TEST_CRED"); !ok || v != "from-file" {
		t.Fatalf("file lookup = %q %v", v, ok)
	}
	t.Setenv("DHI_TEST_CRED", "from-env")
	if v, _ := DefaultLookup("DHI_TEST_CRED"); v != "from-env" {
		t.Fatalf("the environment must win, got %q", v)
	}
}

func TestStdioServerGetsAPrivateHomeAndTmp(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mcp-home")
	b := &Bridge{deps: Deps{HomeDir: home}}
	env, err := b.stdioEnv(mcpserver.Server{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HOME="+home) || !strings.Contains(joined, "TMPDIR="+filepath.Join(home, "tmp")) {
		t.Fatalf("env = %v", env)
	}
	if fi, err := os.Stat(filepath.Join(home, "tmp")); err != nil || !fi.IsDir() {
		t.Fatalf("tmp dir not created: %v", err)
	}
	// Without a HomeDir the server keeps the process environment untouched.
	plain, _ := (&Bridge{}).stdioEnv(mcpserver.Server{})
	if strings.Contains(strings.Join(plain, "\n"), "HOME=") {
		t.Fatalf("HOME set without a HomeDir: %v", plain)
	}
}
