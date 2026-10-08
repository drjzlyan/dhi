package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// lineWriter adapts an io.Writer to the conn.writer seam, appending the
// newline that frames each JSON-RPC message on the stdio transport.
type lineWriter struct{ w io.Writer }

func (l lineWriter) write(b []byte) error {
	if _, err := l.w.Write(b); err != nil {
		return err
	}
	_, err := l.w.Write([]byte("\n"))
	return err
}

// Stdio runs one MCP server as a child process speaking newline-framed
// JSON-RPC on stdin/stdout. Env is the child's environment; callers pass
// toolchain.Manager.Env(nil) so servers resolve hermetic shims (ADR-0005).
type Stdio struct {
	cmd    *exec.Cmd
	conn   *conn
	stdin  io.WriteCloser
	closed sync.Once
	stderr *tailBuffer
}

// tailBuffer keeps the last max bytes written to it: the child's stderr,
// so a server that dies on start can say why.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// DialStdio spawns argv and completes the MCP handshake.
func DialStdio(ctx context.Context, env []string, argv ...string) (*Stdio, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("mcp: empty stdio argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if env != nil {
		cmd.Env = env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdout: %w", err)
	}
	tail := &tailBuffer{max: 2048}
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %s: %w", argv[0], err)
	}
	s := &Stdio{cmd: cmd, conn: newConn(lineWriter{stdin}), stdin: stdin, stderr: tail}
	go s.readLoop(stdout)
	if err := s.conn.handshake(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Stdio) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		s.conn.handleLine(sc.Bytes())
	}
	// stdout closed: the server is gone. Say so — with its own words.
	why := "server exited"
	if e := s.stderr.String(); e != "" {
		why += ": " + lastLines(e, 6)
	}
	s.conn.fail(fmt.Errorf("mcp: %s", why))
}

// lastLines returns the last n lines of s, joined with " | ".
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// Tools implements Caller.
func (s *Stdio) Tools(ctx context.Context) ([]ToolInfo, error) { return s.conn.listTools(ctx) }

// CallTool implements Caller.
func (s *Stdio) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	return s.conn.callTool(ctx, name, args)
}

// Close kills the server process.
func (s *Stdio) Close() error {
	var err error
	s.closed.Do(func() {
		if s.cmd.Process != nil {
			_ = s.stdin.Close()
			err = s.cmd.Process.Kill()
			_, _ = s.cmd.Process.Wait()
		}
	})
	return err
}
