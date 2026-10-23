package dap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"time"

	"github.com/drjzlyan/dhi/internal/testrun"
)

// VerifiedDelve is the delve release confirmed to speak the DAP dialect
// this client expects through initialize → launch → build.
const VerifiedDelve = "v1.27.2"

// ErrNoDelve says dlv is not installed; the editor offers to install it
// with the user's package manager (F-110).
var ErrNoDelve = errors.New("debugging needs the delve adapter (dlv), which is not installed")

// listenLine is what `dlv dap` prints once it accepts connections.
var listenLine = regexp.MustCompile(`DAP server listening at:\s*(\S+)`)

// adapterStartTimeout bounds waiting for the adapter to announce itself.
var adapterStartTimeout = 15 * time.Second

// procConn is a TCP connection that also owns the adapter process.
type procConn struct {
	net.Conn
	cmd *exec.Cmd
}

func (p *procConn) Close() error {
	err := p.Conn.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.cmd.Wait()
	return err
}

// StartDelve launches `dlv dap` (resolved from env's PATH, the user's
// own) in dir and returns a Client connected to it. Closing the Client
// stops the adapter.
func StartDelve(ctx context.Context, dir string, env []string) (*Client, error) {
	bin, err := testrun.LookPath("dlv", env)
	if err != nil {
		return nil, ErrNoDelve
	}
	cmd := exec.Command(bin, "dap", "--listen=127.0.0.1:0")
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("dlv: %w", err)
	}
	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if m := listenLine.FindStringSubmatch(sc.Text()); m != nil {
				addrCh <- m[1]
				break
			}
		}
		_, _ = io.Copy(io.Discard, out) // keep the pipe drained
	}()
	kill := func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	var addr string
	select {
	case addr = <-addrCh:
	case <-time.After(adapterStartTimeout):
		kill()
		return nil, fmt.Errorf("dlv did not announce a DAP address within %s", adapterStartTimeout)
	case <-ctx.Done():
		kill()
		return nil, ctx.Err()
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		kill()
		return nil, fmt.Errorf("dlv: connect %s: %w", addr, err)
	}
	return New(&procConn{Conn: conn, cmd: cmd}), nil
}
