package editor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/lsp"
)

// goplusFake is a minimal scripted language server over one pipe end.
// It echoes back the URIs it receives so fixtures stay path-agnostic.
type goplusFake struct {
	conn net.Conn
	rd   *bufio.Reader

	mu       sync.Mutex
	lastOpen string // file URI seen in the last didOpen
}

func startFakeServer(t *testing.T) (*goplusFake, *lsp.Manager) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	f := &goplusFake{conn: serverConn, rd: bufio.NewReader(serverConn)}
	go f.serve()

	mgr := lsp.NewManager("", nil)
	c, err := lsp.New(context.Background(), clientConn, "/ws")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Inject("go", c)
	t.Cleanup(func() { mgr.ShutdownAll() })
	return f, mgr
}

func (f *goplusFake) serve() {
	for {
		payload, err := readLSPFrame(f.rd)
		if err != nil {
			return
		}
		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			f.reply(*msg.ID, map[string]any{"serverInfo": map[string]any{"name": "fake-gopls"}})

		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.lastOpen = p.TextDocument.URI
			f.mu.Unlock()
			f.notify("textDocument/publishDiagnostics", map[string]any{
				"uri": p.TextDocument.URI,
				"diagnostics": []map[string]any{
					{"range": map[string]any{"start": map[string]any{"line": 0, "character": 0}}, "severity": 1, "message": "boom"},
				},
			})

		case "textDocument/completion":
			f.reply(*msg.ID, []map[string]any{
				{"label": "Println", "detail": "func(a ...any) (n int, err error)"},
				{"label": "Printf"},
			})
		default:
			if msg.ID != nil {
				f.reply(*msg.ID, nil)
			}
		}
	}
}

func (f *goplusFake) reply(id int64, result any) {
	f.writeJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (f *goplusFake) notify(method string, params any) {
	f.writeJSON(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (f *goplusFake) writeJSON(v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(f.conn, "Content-Length: %d\r\n\r\n", len(data))
	f.conn.Write(data)
}

func readLSPFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			fmt.Sscanf(strings.TrimSpace(line[15:]), "%d", &length)
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("no content-length")
	}
	buf := make([]byte, length)
	readFull(r, buf)
	return buf, nil
}

func readFull(r *bufio.Reader, buf []byte) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return
		}
	}
}

func waitFor(t *testing.T, m *Model, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.drainTerm()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s:\n%s", what, plainView(m))
}

func TestLSPWithFakeServer(t *testing.T) {
	srv, mgr := startFakeServer(t)

	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)

	// open main.go → didOpen → server echoes diagnostics for its URI
	feed(m, "enter", "down", "enter", "down", "down", "enter")
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")

	// completion popup in insert mode
	feed(m, "i")
	feed(m, "ctrl+space")
	waitFor(t, m, func() bool {
		v := plainView(m)
		return strings.Contains(v, "completions:") && strings.Contains(v, "Println")
	}, "completion popup")

	feed(m, "enter") // accept Println
	if !strings.Contains(m.active().Buffer().Text(), "Println") {
		t.Errorf("accepted label not inserted:\n%q", m.active().Buffer().Text())
	}
	_ = srv.lastOpen
}

func TestLSPSilentWithoutManager(t *testing.T) {
	m := newEditor(t) // no WithLSP
	feed(m, "enter", "down", "down", "enter")
	feed(m, "i")
	feed(m, "ctrl+space")
	m.drainTerm()
	if m.compOpen || strings.Contains(plainView(m), "completions:") {
		t.Error("completion fired without a manager")
	}
}
