package lsp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Manager owns one client per language, resolving servers through the
// hermetic toolchain shim dir. A language without an installed server
// simply has no LSP support — never a host-tool fallback (ADR-0005).
type Manager struct {
	shimDir string
	managed string // DHI-installed servers: <managed>/<lang>/node_modules/.bin
	env     []string

	mu      sync.Mutex
	clients map[string]*Client
}

// NewManager builds a manager; env is the child-process environment
// (toolchain.Manager.Env output).
func NewManager(shimDir string, env []string) *Manager {
	return &Manager{shimDir: shimDir, env: env, clients: map[string]*Client{}}
}

// SetManagedRoot names the folder holding servers DHI installed itself, one
// sub-folder per language (F-050).
func (m *Manager) SetManagedRoot(dir string) { m.managed = dir }

// ManagedPrefix is the npm prefix a language's server installs into.
func (m *Manager) ManagedPrefix(lang string) string {
	if m.managed == "" {
		return ""
	}
	return filepath.Join(m.managed, lang)
}

// Locate finds a server binary without ever consulting the host PATH: an
// absolute `server` is the user's explicit choice; a bare name is looked up
// in the toolchain shim dir, then in the language's managed install.
func (m *Manager) Locate(lang, server string) (string, bool) {
	if filepath.IsAbs(server) {
		_, err := os.Stat(server)
		return server, err == nil
	}
	if p := m.ShimPath(server); fileExists(p) {
		return p, true
	}
	if m.managed != "" {
		if p := filepath.Join(m.managed, lang, "node_modules", ".bin", server); fileExists(p) {
			return p, true
		}
	}
	return "", false
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ShimPath is where the named server binary must live.
func (m *Manager) ShimPath(server string) string {
	return filepath.Join(m.shimDir, server)
}

// Available reports whether the server binary exists.
func (m *Manager) Available(server string) bool {
	_, err := os.Stat(m.ShimPath(server))
	return err == nil
}

// Ensure returns a live client for lang, starting `server` on first use.
// rootDir seeds workspace roots in initialize.
func (m *Manager) Ensure(ctx context.Context, lang, server, rootDir string) (*Client, error) {
	return m.EnsureServer(ctx, lang, server, nil, rootDir)
}

// EnsureServer is Ensure with the server's arguments (most npm servers need
// --stdio) and the managed-install lookup of Locate.
func (m *Manager) EnsureServer(ctx context.Context, lang, server string, args []string, rootDir string) (*Client, error) {
	m.mu.Lock()
	if c, ok := m.clients[lang]; ok {
		select {
		case <-c.Done():
			delete(m.clients, lang) // died; restart below
		default:
			m.mu.Unlock()
			return c, nil
		}
	}
	m.mu.Unlock()

	bin, ok := m.Locate(lang, server)
	if !ok {
		return nil, fmt.Errorf("lsp: %s not installed (%s)", server, m.ShimPath(server))
	}
	c, err := StartProcess(ctx, bin, append([]string{}, args...), m.env, rootDir)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.clients[lang] = c
	m.mu.Unlock()
	return c, nil
}

// Inject registers a pre-built client (tests, custom transports).
func (m *Manager) Inject(lang string, c *Client) {
	m.mu.Lock()
	m.clients[lang] = c
	m.mu.Unlock()
}

// ClientFor returns the cached client for lang (nil when none).
func (m *Manager) ClientFor(lang string) *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clients[lang]
}

// ShutdownAll politely tears every client down.
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	cs := make([]*Client, 0, len(m.clients))
	for _, c := range m.clients {
		cs = append(cs, c)
	}
	m.clients = map[string]*Client{}
	m.mu.Unlock()
	for _, c := range cs {
		c.Shutdown()
	}
}

// StartProcess spawns a server binary with stdio wired to the client.
func StartProcess(ctx context.Context, bin string, args, env []string, rootDir string) (*Client, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	if len(env) == 0 {
		cmd.Env = os.Environ()
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdout: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start %s: %w", filepath.Base(bin), err)
	}
	conn := &processConn{cmd: cmd, r: stdout, w: stdin}
	c, err := New(ctx, conn, rootDir)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	go func() { <-c.Done(); _ = cmd.Wait() }()
	return c, nil
}

// processConn adapts command pipes to io.ReadWriteCloser.
type processConn struct {
	cmd   *exec.Cmd
	r     io.ReadCloser
	w     io.WriteCloser
	close sync.Once
}

func (p *processConn) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *processConn) Write(b []byte) (int, error) { return p.w.Write(b) }

func (p *processConn) Close() error {
	var err error
	p.close.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		err = p.w.Close()
	})
	return err
}
