package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Diagnostic is one published problem for a file.
type Diagnostic struct {
	Path     string
	Line     int // 0-based
	Col      int // 0-based rune-ish (server byte col; display only)
	Severity int // 1 error, 2 warning, 3 info, 4 hint
	Message  string
}

// CompletionItem is one completion candidate.
type CompletionItem struct {
	Label  string
	Detail string
}

// EventKind discriminates async server pushes.
type EventKind uint8

const (
	EvDiagnostics EventKind = iota
)

// Event is one server→client notification routed to the UI.
type Event struct {
	Kind  EventKind
	Diags []Diagnostic // populated for EvDiagnostics (whole-file set)
}

// Client talks to one language server over a duplex connection.
type Client struct {
	conn    io.ReadWriteCloser
	reader  *bufio.Reader
	writeMu sync.Mutex

	nextID   atomic.Int64
	pending  map[int64]chan rpcMessage
	pendingM sync.Mutex

	Events <-chan Event
	events chan Event

	ctx        context.Context
	cancel     context.CancelFunc
	initDone   bool
	serverName string
}

// New wraps an established connection and runs the handshake.
func New(ctx context.Context, conn io.ReadWriteCloser, rootDir string) (*Client, error) {
	c := &Client{
		conn:    conn,
		reader:  bufio.NewReader(conn),
		pending: map[int64]chan rpcMessage{},
		events:  make(chan Event, 64),
	}
	c.Events = c.events
	c.ctx, c.cancel = context.WithCancel(ctx)
	// The reader must run BEFORE the handshake: responses are routed by
	// the read loop and would otherwise deadlock initialize.
	go c.readLoop()
	if err := c.initialize(rootDir); err != nil {
		c.cancel()
		return nil, err
	}
	return c, nil
}

func (c *Client) initialize(rootDir string) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   pathToURI(rootDir),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"completion": map[string]any{"completionItem": map[string]any{}},
			},
		},
	}
	var result struct {
		ServerInfo *struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := c.call("initialize", params, &result); err != nil {
		return fmt.Errorf("lsp: initialize: %w", err)
	}
	if result.ServerInfo != nil {
		c.serverName = result.ServerInfo.Name
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	c.initDone = true
	return nil
}

// ServerName reports the server's declared name ("" before init).
func (c *Client) ServerName() string { return c.serverName }

func (c *Client) call(method string, params, result any) error {
	return c.callCtx(c.ctx, method, params, result)
}

func (c *Client) callCtx(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	payload, err := encodeRequest(id, method, params)
	if err != nil {
		return err
	}
	ch := make(chan rpcMessage, 1)
	c.pendingM.Lock()
	c.pending[id] = ch
	c.pendingM.Unlock()

	if err := c.write(payload); err != nil {
		c.pendingM.Lock()
		delete(c.pending, id)
		c.pendingM.Unlock()
		return err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("lsp: %s: %s", method, resp.Error.Message)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("lsp: %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) write(payload []byte) error {
	if err := ctxErr(c.ctx); err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFrame(c.conn, payload)
}

func (c *Client) notify(method string, params any) error {
	payload, err := encodeNotification(method, params)
	if err != nil {
		return err
	}
	return c.write(payload)
}

// readLoop routes responses to pending calls and notifications to Events.
func (c *Client) readLoop() {
	defer close(c.events)
	for {
		if err := c.ctx.Err(); err != nil {
			return
		}
		payload, err := readFrame(c.reader)
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		switch {
		case msg.ID != nil && msg.Method == "":
			c.pendingM.Lock()
			ch := c.pending[*msg.ID]
			delete(c.pending, *msg.ID)
			c.pendingM.Unlock()
			if ch != nil {
				ch <- msg
			}
		case msg.ID != nil && msg.Method != "":
			// server→client request we don't implement; reply empty so
			// the server never stalls waiting on us.
			resp, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage("null")})
			_ = c.write(resp)
		case msg.Method == "textDocument/publishDiagnostics":
			ev, ok := decodeDiagnostics(msg.Params)
			if !ok {
				continue
			}
			select {
			case c.events <- ev:
			default: // never block the reader on a slow UI
			}
		}
	}
}

func decodeDiagnostics(params json.RawMessage) (Event, bool) {
	var p struct {
		URI  string `json:"uri"`
		Diag []struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
			Severity int    `json:"severity"`
			Message  string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Event{}, false
	}
	ev := Event{Kind: EvDiagnostics, Diags: make([]Diagnostic, 0, len(p.Diag))}
	path := uriToPath(p.URI)
	for _, d := range p.Diag {
		ev.Diags = append(ev.Diags, Diagnostic{
			Path:     path,
			Line:     d.Range.Start.Line,
			Col:      d.Range.Start.Character,
			Severity: d.Severity,
			Message:  d.Message,
		})
	}
	return ev, true
}

// DidOpen announces a newly opened document (full-text sync).
func (c *Client) DidOpen(path, text string, languageID string) error {
	return c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path), "languageId": languageID, "version": 1, "text": text},
	})
}

// DidChange pushes the full new text (sync kind: full).
func (c *Client) DidChange(path, text string) error {
	return c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": pathToURI(path), "version": c.version()},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

var docVersions atomic.Int64

func (c *Client) version() int64 { return docVersions.Add(1) }

// Completion requests candidates at a 0-based line/col.
func (c *Client) Completion(path string, line, col int) ([]CompletionItem, error) {
	var result struct {
		Items []struct {
			Label  string `json:"label"`
			Detail string `json:"detail"`
		} `json:"items"`
		Raw []CompletionItem
	}
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := c.call("textDocument/completion", params, &raw); err != nil {
		return nil, err
	}
	// result may be Item[] or {items:[…]}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil && list != nil {
		return itemsFrom(list), nil
	}
	if err := json.Unmarshal(raw, &result); err == nil {
		out := make([]CompletionItem, 0, len(result.Items))
		for _, it := range result.Items {
			out = append(out, CompletionItem{Label: it.Label, Detail: it.Detail})
		}
		return out, nil
	}
	return nil, nil
}

func itemsFrom(list []map[string]any) []CompletionItem {
	out := make([]CompletionItem, 0, len(list))
	for _, m := range list {
		item := CompletionItem{}
		if l, ok := m["label"].(string); ok {
			item.Label = l
		}
		if d, ok := m["detail"].(string); ok {
			item.Detail = d
		}
		out = append(out, item)
	}
	return out
}

// Shutdown performs the polite two-step teardown.
func (c *Client) Shutdown() {
	// Bounded wait: never let teardown hang on a misbehaving server.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.callCtx(ctx, "shutdown", nil, nil)
	_ = c.notify("exit", nil)
	c.cancel()
	_ = c.conn.Close()
}

// Done exposes context state for owners checking liveness.
func (c *Client) Done() <-chan struct{} { return c.ctx.Done() }

// URI helpers (plain file:// mapping; no percent-encoding edge cases in
// workspace paths DHI manages).

func pathToURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

func uriToPath(uri string) string {
	return strings.TrimPrefix(filepath.FromSlash(strings.TrimPrefix(uri, "file://")), "")
}
