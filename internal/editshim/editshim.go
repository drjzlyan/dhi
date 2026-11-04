// Package editshim lets a program that wants an editor (git rebase -i, a
// commit message) open its file in the running DHI instead of a nested vim
// (F-165).
//
// The program is started with GIT_EDITOR and GIT_SEQUENCE_EDITOR naming a
// symlink called dhi-edit in a private temp folder; the link points at the DHI
// binary. When DHI starts as "dhi-edit" (see IsShim) it only asks the running
// instance, over a unix socket in that same folder, to open the file, and
// blocks until the buffer is closed: exit 0 for a :wq (or :q), exit 1 for a
// :cq. It is not a subcommand: nothing a user types reaches it.
package editshim

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// ShimName is the basename DHI is started under to act as the editor.
	ShimName = "dhi-edit"
	// SockEnv names the socket a shim connects to.
	SockEnv = "DHI_EDIT_SOCK"
)

// IsShim reports whether the program was started as the editor shim.
func IsShim(arg0 string) bool { return filepath.Base(arg0) == ShimName }

// Request is one file a program wants edited. The owner answers it once with
// Done: true lets the program carry on, false makes it abort the step.
type Request struct {
	Path string
	conn net.Conn
	once *sync.Once
}

// Done answers the waiting shim.
func (r Request) Done(ok bool) {
	r.once.Do(func() {
		reply := "abort\n"
		if ok {
			reply = "ok\n"
		}
		_, _ = r.conn.Write([]byte(reply))
		_ = r.conn.Close()
	})
}

// Server accepts edit requests from shims.
type Server struct {
	dir   string
	sock  string
	ln    net.Listener
	reqs  chan Request
	quit  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	conns map[net.Conn]bool
}

// Start listens on a fresh private folder and links exe into it as the
// shim. exe is the DHI binary (os.Executable).
func Start(exe string) (*Server, error) {
	// The socket path must stay short (about 100 bytes on macOS), so the
	// folder is under the OS temp dir, owner-only, and removed on Close.
	dir, err := os.MkdirTemp("", "dhi-edit-")
	if err != nil {
		return nil, err
	}
	s := &Server{dir: dir, sock: filepath.Join(dir, "s"), reqs: make(chan Request), quit: make(chan struct{}), conns: map[net.Conn]bool{}}
	if err := os.Symlink(exe, filepath.Join(dir, ShimName)); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	ln, err := net.Listen("unix", s.sock)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("edit socket: %w", err)
	}
	s.ln = ln
	go s.accept()
	return s, nil
}

// Env is the environment that makes a program use the shim.
func (s *Server) Env() []string {
	cmd := "'" + strings.ReplaceAll(filepath.Join(s.dir, ShimName), "'", `'\''`) + "'"
	return []string{"GIT_SEQUENCE_EDITOR=" + cmd, "GIT_EDITOR=" + cmd, SockEnv + "=" + s.sock}
}

// Requests delivers the files to edit; receive until Closed fires.
func (s *Server) Requests() <-chan Request { return s.reqs }

// Closed is closed once the server stops.
func (s *Server) Closed() <-chan struct{} { return s.quit }

// Close stops listening and removes the folder. Waiting shims exit non-zero.
func (s *Server) Close() {
	s.once.Do(func() {
		close(s.quit)
		_ = s.ln.Close()
		s.mu.Lock()
		for c := range s.conns { // a shim still waiting exits non-zero
			_ = c.Close()
		}
		s.mu.Unlock()
		_ = os.RemoveAll(s.dir)
	})
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

func (s *Server) serve(c net.Conn) {
	s.mu.Lock()
	s.conns[c] = true
	s.mu.Unlock()
	line, err := bufio.NewReader(c).ReadString('\n')
	path := strings.TrimRight(line, "\n")
	if err != nil || path == "" {
		_ = c.Close()
		return
	}
	req := Request{Path: path, conn: c, once: new(sync.Once)}
	select {
	case s.reqs <- req:
	case <-s.quit:
		_ = c.Close()
	}
}

// RunShim is the shim's whole program: ask the DHI named by DHI_EDIT_SOCK to
// edit args[0] and wait. It returns the exit status.
func RunShim(args []string, getenv func(string) string, stderr func(string)) int {
	sock := getenv(SockEnv)
	if sock == "" || len(args) < 1 {
		stderr("dhi-edit: only runs for a program that DHI started")
		return 2
	}
	path, err := filepath.Abs(args[0])
	if err != nil {
		stderr("dhi-edit: " + err.Error())
		return 2
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		stderr("dhi-edit: DHI is not listening: " + err.Error())
		return 2
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write([]byte(path + "\n")); err != nil {
		stderr("dhi-edit: " + err.Error())
		return 2
	}
	reply, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		stderr("dhi-edit: DHI closed before the edit finished")
		return 1
	}
	if strings.TrimSpace(reply) == "ok" {
		return 0
	}
	stderr("dhi-edit: edit cancelled (:cq)")
	return 1
}
