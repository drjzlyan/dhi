// Package dapfake is a scripted Debug Adapter Protocol server for tests
// (internal/dap and the editor). It speaks just enough DAP to run the
// launch handshake, stop at a breakpoint with one stack, step, continue
// to exit, evaluate, and disconnect.
package dapfake

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// Adapter is one scripted adapter attached to a pipe.
type Adapter struct {
	conn net.Conn
	rd   *bufio.Reader

	mu       sync.Mutex
	seq      int
	requests []string
	bpLines  map[string][]int
	launch   map[string]any
	launchID int
	fail     map[string]string
	// StopPath/StopLine place the first frame of every stop.
	stopPath string
	stopLine int
}

// New returns the client end of a pipe wired to a running Adapter. The
// stop location defaults to /ws/a.go:10.
func New(t *testing.T) (net.Conn, *Adapter) {
	t.Helper()
	cc, sc := net.Pipe()
	a := &Adapter{conn: sc, rd: bufio.NewReader(sc), bpLines: map[string][]int{}, fail: map[string]string{},
		stopPath: "/ws/a.go", stopLine: 10}
	go a.serve()
	t.Cleanup(func() { _ = cc.Close(); _ = sc.Close() })
	return cc, a
}

// StopAt sets where the next stops report their top frame.
func (a *Adapter) StopAt(path string, line int) {
	a.mu.Lock()
	a.stopPath, a.stopLine = path, line
	a.mu.Unlock()
}

// Fail makes cmd answer with an error response.
func (a *Adapter) Fail(cmd, msg string) {
	a.mu.Lock()
	a.fail[cmd] = msg
	a.mu.Unlock()
}

// Seen lists received commands in order.
func (a *Adapter) Seen() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.requests...)
}

// Breakpoints returns the last lines set for path.
func (a *Adapter) Breakpoints(path string) []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]int(nil), a.bpLines[path]...)
}

// Launch returns the arguments of the launch request.
func (a *Adapter) Launch() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.launch
}

// Drop closes the server end (simulates the adapter dying).
func (a *Adapter) Drop() { _ = a.conn.Close() }

func (a *Adapter) write(v map[string]any) {
	a.mu.Lock()
	a.seq++
	v["seq"] = a.seq
	a.mu.Unlock()
	data, _ := json.Marshal(v)
	fmt.Fprintf(a.conn, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

func (a *Adapter) respond(req map[string]any, body any) {
	cmd, _ := req["command"].(string)
	a.mu.Lock()
	msg, bad := a.fail[cmd]
	a.mu.Unlock()
	if bad {
		// Like delve: a short category in `message`, the reason in body.error.
		short, detail := msg, ""
		if i := strings.Index(msg, ": "); i > 0 {
			short, detail = msg[:i], msg
		}
		resp := map[string]any{"type": "response", "request_seq": req["seq"], "success": false, "command": cmd, "message": short}
		if detail != "" {
			resp["body"] = map[string]any{"error": map[string]any{"id": 3000, "format": detail}}
		}
		a.write(resp)
		return
	}
	a.write(map[string]any{"type": "response", "request_seq": req["seq"], "success": true, "command": cmd, "body": body})
}

func (a *Adapter) event(name string, body any) {
	a.write(map[string]any{"type": "event", "event": name, "body": body})
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(k, "Content-Length") {
			fmt.Sscanf(strings.TrimSpace(v), "%d", &length)
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("no content-length")
	}
	buf := make([]byte, length)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func (a *Adapter) serve() {
	for {
		payload, err := readFrame(a.rd)
		if err != nil {
			return
		}
		var req map[string]any
		if json.Unmarshal(payload, &req) != nil {
			continue
		}
		cmd, _ := req["command"].(string)
		a.mu.Lock()
		a.requests = append(a.requests, cmd)
		stopPath, stopLine := a.stopPath, a.stopLine
		a.mu.Unlock()
		args, _ := req["arguments"].(map[string]any)
		switch cmd {
		case "initialize":
			a.respond(req, map[string]any{"supportsConfigurationDoneRequest": true})
		case "launch":
			a.mu.Lock()
			a.launch, a.launchID = args, int(req["seq"].(float64))
			_, refuse := a.fail["launch"]
			a.mu.Unlock()
			if refuse {
				// A refused launch answers with an error and never sends
				// `initialized` (what delve does when it cannot start).
				a.respond(req, nil)
				break
			}
			a.event("initialized", nil)
		case "setBreakpoints":
			src, _ := args["source"].(map[string]any)
			path, _ := src["path"].(string)
			var lines []int
			var out []map[string]any
			for _, b := range args["breakpoints"].([]any) {
				l := int(b.(map[string]any)["line"].(float64))
				lines = append(lines, l)
				ok := l != 999
				msg := ""
				if !ok {
					msg = "no code here"
				}
				out = append(out, map[string]any{"verified": ok, "line": l, "message": msg})
			}
			a.mu.Lock()
			a.bpLines[path] = lines
			a.mu.Unlock()
			a.respond(req, map[string]any{"breakpoints": out})
		case "configurationDone":
			a.respond(req, nil)
			a.mu.Lock()
			id := a.launchID
			a.mu.Unlock()
			a.write(map[string]any{"type": "response", "request_seq": id, "success": true, "command": "launch"})
			a.event("output", map[string]any{"category": "stdout", "output": "hello\nworld\n"})
			a.event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1})
		case "stackTrace":
			a.respond(req, map[string]any{"stackFrames": []map[string]any{
				{"id": 1000, "name": "main.run", "line": stopLine, "source": map[string]any{"path": stopPath}},
				{"id": 1001, "name": "main.main", "line": 4, "source": map[string]any{"path": stopPath}},
			}})
		case "scopes":
			a.respond(req, map[string]any{"scopes": []map[string]any{
				{"name": "Arguments", "variablesReference": 4},
				{"name": "Locals", "variablesReference": 5},
			}})
		case "variables":
			a.respond(req, map[string]any{"variables": []map[string]any{{"name": "x", "value": "42", "type": "int"}}})
		case "next", "stepIn", "stepOut":
			a.respond(req, nil)
			a.event("stopped", map[string]any{"reason": "step", "threadId": 1})
		case "continue":
			a.respond(req, map[string]any{"allThreadsContinued": true})
			a.event("continued", map[string]any{"threadId": 1})
			a.event("exited", map[string]any{"exitCode": 3})
			a.event("terminated", nil)
		case "evaluate":
			a.respond(req, map[string]any{"result": "84", "type": "int"})
		case "disconnect":
			a.respond(req, nil)
			_ = a.conn.Close()
			return
		default:
			a.respond(req, nil)
		}
	}
}
