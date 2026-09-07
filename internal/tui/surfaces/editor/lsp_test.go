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

		case "textDocument/hover":
			f.reply(*msg.ID, map[string]any{"contents": map[string]any{
				"kind": "markdown", "value": "```go\nfunc Helper()\n```\nhover doc line2",
			}})

		case "textDocument/rename":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				NewName string `json:"newName"`
			}
			json.Unmarshal(msg.Params, &p)
			f.reply(*msg.ID, map[string]any{"changes": map[string]any{
				p.TextDocument.URI: []map[string]any{
					{"range": map[string]any{
						"start": map[string]any{"line": 0, "character": 0},
						"end":   map[string]any{"line": 0, "character": 7},
					}, "newText": p.NewName},
				},
			}})

		case "textDocument/codeAction":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			json.Unmarshal(msg.Params, &p)
			f.reply(*msg.ID, []map[string]any{
				{"title": "quickfix boom", "edit": map[string]any{"changes": map[string]any{
					p.TextDocument.URI: []map[string]any{
						{"range": map[string]any{
							"start": map[string]any{"line": 1, "character": 0},
							"end":   map[string]any{"line": 1, "character": 0},
						}, "newText": "fixed\n"},
					},
				}}},
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

// request sends a server→client request to the editor's client.
func (f *goplusFake) request(id int64, method string, params any) {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	fmt.Fprintf(f.conn, "Content-Length: %d\r\n\r\n", len(data))
	f.conn.Write(data)
}

func openMainGoWithLSP(t *testing.T) (*Model, *goplusFake) {
	t.Helper()
	srv, mgr := startFakeServer(t)
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithLSP(mgr))
	m.Resize(100, 30)
	feed(m, "enter", "down", "enter", "down", "down", "enter") // open main.go
	waitFor(t, m, func() bool { return strings.Contains(plainView(m), "✗1") }, "diagnostic chip")
	return m, srv
}

func TestLSPHoverPopup(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "K")
	waitFor(t, m, func() bool {
		v := plainView(m)
		return strings.Contains(v, "hover:") && strings.Contains(v, "hover doc")
	}, "hover popup")
	feed(m, "j") // movement closes hover
	if m.hoverOpen {
		t.Error("hover should close on cursor movement")
	}
}

func TestLSPRenameFlow(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "g", "r")
	if !m.renameMode {
		t.Fatal("rename input did not open")
	}
	typeKeys(m, "zeta")
	feed(m, "enter")
	waitFor(t, m, func() bool {
		return strings.Contains(m.active().Buffer().Text(), "zeta main")
	}, "rename applied")
}

func TestLSPCodeActions(t *testing.T) {
	m, _ := openMainGoWithLSP(t)
	feed(m, "g", "a")
	waitFor(t, m, func() bool {
		v := plainView(m)
		return strings.Contains(v, "code actions:") && strings.Contains(v, "quickfix boom")
	}, "action popup")
	feed(m, "enter")
	waitFor(t, m, func() bool {
		return strings.Contains(m.active().Buffer().Text(), "fixed")
	}, "action edit applied")
}

func TestLSPApplyEditPush(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	uri := "file://" + m.active().Path()
	srv.request(77, "workspace/applyEdit", map[string]any{
		"edit": map[string]any{"changes": map[string]any{
			uri: []map[string]any{
				{"range": map[string]any{
					"start": map[string]any{"line": 0, "character": 0},
					"end":   map[string]any{"line": 0, "character": 0},
				}, "newText": "// touched\n"},
			},
		}},
	})
	waitFor(t, m, func() bool {
		return strings.Contains(m.active().Buffer().Text(), "// touched")
	}, "server push applied")
}

func TestLSPDiagnosticsClear(t *testing.T) {
	m, srv := openMainGoWithLSP(t)
	srv.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         "file://" + m.active().Path(),
		"diagnostics": []map[string]any{},
	})
	waitFor(t, m, func() bool { return !strings.Contains(plainView(m), "✗1") }, "chip cleared")
}

func TestLSPInertWithoutServer(t *testing.T) {
	m := newEditor(t) // no WithLSP: K/gr/ga must be no-ops
	feed(m, "enter", "down", "down", "enter")
	feed(m, "K", "g", "r", "g", "a")
	m.drainTerm()
	if m.hoverOpen || m.renameMode || m.actionOpen {
		t.Error("lsp flows opened without a manager")
	}
}

func TestWordAt(t *testing.T) {
	cases := []struct {
		line string
		col  int
		word string
	}{
		{"package main", 0, "package"},
		{"package main", 3, "package"},
		{"package main", 7, "package"}, // space after word still finds it
		{"package main", 11, "main"},
		{"ab cd", 2, "ab"},
		{"", 0, ""},
		{"  x", 2, "x"},
	}
	for _, c := range cases {
		w, _, _ := wordAt(c.line, c.col)
		if w != c.word {
			t.Errorf("wordAt(%q, %d) = %q, want %q", c.line, c.col, w, c.word)
		}
	}
}
