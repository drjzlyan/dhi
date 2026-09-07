// Package term runs PTY-backed terminal sessions for the editor drawer.
// Each session spawns a shell pinned to a working directory with DHI's
// hermetic toolchain PATH (toolchain.Manager.Env — ADR-0005), streams
// output to a channel, and supports write/resize/kill. Output is
// delivered raw; the drawer strips ANSI for its MVP scrollback (full
// VT emulation is deferred).
package term

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/creack/pty"
)

// Session is one live PTY.
type Session struct {
	cmd   *exec.Cmd
	tty   *os.File // pty master
	dir   string
	label string

	mu     sync.Mutex
	closed bool

	Out <-chan []byte
}

// Options configures a new session.
type Options struct {
	Dir     string   // working directory (member repo root)
	Label   string   // tab label
	Env     []string // full environment (use toolchain Manager.Env)
	Command []string // override argv; default = user shell
}

// Start launches a session; output flows on s.Out until exit.
// Strict (ADR-0011): an empty Env refuses — a DHI child process must
// never silently inherit the host environment. Callers without a
// toolchain pass os.Environ() explicitly (opt-out), production passes
// toolchain.Manager.Env.
func Start(ctx context.Context, opt Options) (*Session, error) {
	if opt.Dir == "" {
		return nil, fmt.Errorf("term: no working directory")
	}
	if info, err := os.Stat(opt.Dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("term: bad dir %s", opt.Dir)
	}
	if len(opt.Env) == 0 {
		return nil, fmt.Errorf("term: hermetic environment unavailable (toolchain not installed — run bootstrap); refusing to leak the host PATH (ADR-0011)")
	}
	argv := opt.Command
	if len(argv) == 0 {
		argv = []string{defaultShell()}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opt.Dir
	cmd.Env = opt.Env

	tty, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("term: start %s: %w", argv[0], err)
	}
	s := &Session{cmd: cmd, tty: tty, dir: opt.Dir, label: opt.Label}

	out := make(chan []byte, 64)
	s.Out = out
	go func() {
		defer close(out)
		buf := make([]byte, 32*1024)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case out <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
	go func() {
		<-ctx.Done()
		s.Close()
	}()
	return s, nil
}

func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" && sh != "/bin/false" {
		return sh
	}
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

// Write sends input to the running program.
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("term: session closed")
	}
	_, err := s.tty.Write(data)
	return err
}

// Resize updates the pty size (drives SIGWINCH).
func (s *Session) Resize(cols, rows int) error {
	if cols < 2 || rows < 2 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return pty.Setsize(s.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Label returns the tab label (defaults to dir base name).
func (s *Session) Label() string {
	if s.label != "" {
		return s.label
	}
	base := s.dir
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return base
}

// Close kills the process and releases the pty. Safe to call twice.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.tty.Close()
}
