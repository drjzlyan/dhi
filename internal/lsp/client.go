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
	Insert string // text to insert (textEdit/insertText, snippets made plain); Label when the server gave none
	Filter string // filterText, when it differs from Label
	Range  *Range // the range Insert replaces (UTF-16 columns); nil = the word before the cursor
}

// EventKind discriminates async server pushes.
type EventKind uint8

const (
	EvDiagnostics EventKind = iota
	EvApplyEdit
)

// Event is one server→client notification routed to the UI.
type Event struct {
	Kind  EventKind
	Path  string         // EvDiagnostics: publishing file (even when empty)
	Diags []Diagnostic   // populated for EvDiagnostics (whole-file set)
	Edit  *WorkspaceEdit // populated for EvApplyEdit
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
	caps       map[string]json.RawMessage // server capabilities; nil = not reported
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
				"hover":      map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
				"signatureHelp": map[string]any{"signatureInformation": map[string]any{
					"documentationFormat":    []string{"plaintext", "markdown"},
					"parameterInformation":   map[string]any{"labelOffsetSupport": true},
					"activeParameterSupport": true,
				}},
				"synchronization": map[string]any{
					"didSave": true,
				},
				"rename":     map[string]any{},
				"codeAction": map[string]any{},
				"formatting": map[string]any{},
			},
			"workspace": map[string]any{
				"applyEdit": true,
			},
		},
	}
	var result struct {
		ServerInfo *struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := c.call("initialize", params, &result); err != nil {
		return fmt.Errorf("lsp: initialize: %w", err)
	}
	if result.ServerInfo != nil {
		c.serverName = result.ServerInfo.Name
	}
	c.caps = result.Capabilities
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	c.initDone = true
	return nil
}

// CanFormat reports whether the server advertises whole-document
// formatting. A server that reports no capabilities at all is given the
// benefit of the doubt (test doubles); one that reports some and omits
// documentFormattingProvider (pyright) is not asked, because it answers
// "Unhandled method".
func (c *Client) CanFormat() bool {
	if c.caps == nil {
		return true
	}
	v, ok := c.caps["documentFormattingProvider"]
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(v))
	return s != "false" && s != "null" && s != ""
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
		case msg.ID != nil && msg.Method == "workspace/applyEdit":
			// Auto-accept (the UI applies asynchronously) and route the
			// edit to the consumer; the reader never blocks on it.
			resp, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{"applied":true}`)})
			_ = c.write(resp)
			if edit, ok := decodeApplyEdit(msg.Params); ok {
				select {
				case c.events <- Event{Kind: EvApplyEdit, Edit: edit}:
				default:
				}
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
	ev := Event{Kind: EvDiagnostics, Path: uriToPath(p.URI), Diags: make([]Diagnostic, 0, len(p.Diag))}
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

// SaveOptions reports whether the server wants didSave notifications and
// whether it wants the full text included. A server that reports no
// capabilities at all is given the benefit of the doubt (test doubles); one
// that reports capabilities but omits save is not asked (ADR-0011).
func (c *Client) SaveOptions() (send, includeText bool) {
	if c.caps == nil {
		return true, false // test doubles benefit of doubt
	}
	raw, ok := c.caps["textDocumentSync"]
	if !ok {
		return false, false
	}
	var s any
	_ = json.Unmarshal(raw, &s)
	switch t := s.(type) {
	case map[string]any:
		// Object with optional `save` field.
		save, ok := t["save"]
		if !ok {
			return false, false
		}
		// `save: true` or `save: {}` means send didSave without text.
		switch sv := save.(type) {
		case bool:
			return sv, false
		case map[string]any:
			// Check for `includeText: true`.
			if it, ok := sv["includeText"].(bool); ok {
				return true, it
			}
			return true, false
		}
		return false, false
	}
	// Plain number or other type: no save capability.
	return false, false
}

// DidSave announces that a document was saved. The server's SaveOptions
// determines whether this is sent and whether text is included.
func (c *Client) DidSave(path, text string) error {
	send, includeText := c.SaveOptions()
	if !send {
		return nil
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
	}
	if includeText {
		params["text"] = text
	}
	return c.notify("textDocument/didSave", params)
}

// DidClose announces that a document was closed.
func (c *Client) DidClose(path string) error {
	return c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
	})
}

var docVersions atomic.Int64

func (c *Client) version() int64 { return docVersions.Add(1) }

// Completion requests candidates at a 0-based line and UTF-16 column.
// The result may be CompletionItem[] or a CompletionList.
func (c *Client) Completion(path string, line, col int) ([]CompletionItem, error) {
	var raw json.RawMessage
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": col},
	}
	if err := c.call("textDocument/completion", params, &raw); err != nil {
		return nil, err
	}
	return decodeCompletion(raw), nil
}

type wireCompletion struct {
	Label      string `json:"label"`
	Detail     string `json:"detail"`
	InsertText string `json:"insertText"`
	FilterText string `json:"filterText"`
	Format     int    `json:"insertTextFormat"` // 2 = snippet
	TextEdit   *struct {
		NewText string `json:"newText"`
		Range   *Range `json:"range"`
		Replace *Range `json:"replace"` // InsertReplaceEdit
	} `json:"textEdit"`
}

func decodeCompletion(raw json.RawMessage) []CompletionItem {
	var list []wireCompletion
	if err := json.Unmarshal(raw, &list); err != nil || list == nil {
		var cl struct {
			Items []wireCompletion `json:"items"`
		}
		if json.Unmarshal(raw, &cl) != nil {
			return nil
		}
		list = cl.Items
	}
	out := make([]CompletionItem, 0, len(list))
	for _, w := range list {
		it := CompletionItem{Label: w.Label, Detail: w.Detail, Insert: w.InsertText, Filter: w.FilterText}
		if w.TextEdit != nil {
			it.Insert = w.TextEdit.NewText
			if r := w.TextEdit.Range; r != nil {
				it.Range = r
			} else if r := w.TextEdit.Replace; r != nil {
				it.Range = r
			}
		}
		if it.Insert == "" {
			it.Insert = w.Label
		}
		if w.Format == 2 {
			it.Insert = StripSnippet(it.Insert)
		}
		out = append(out, it)
	}
	return out
}

// StripSnippet turns snippet syntax ($1, ${1:name}, $0) into plain text,
// keeping placeholders' default text.
func StripSnippet(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		switch {
		case r[i] == '\\' && i+1 < len(r):
			i++
			b.WriteRune(r[i])
		case r[i] == '$' && i+1 < len(r) && r[i+1] >= '0' && r[i+1] <= '9':
			for i+1 < len(r) && r[i+1] >= '0' && r[i+1] <= '9' {
				i++
			}
		case r[i] == '$' && i+1 < len(r) && r[i+1] == '{':
			j := i + 2
			for j < len(r) && r[j] >= '0' && r[j] <= '9' {
				j++
			}
			if j < len(r) && r[j] == ':' {
				j++
			}
			depth := 1
			k := j
			for k < len(r) && depth > 0 {
				switch r[k] {
				case '{':
					depth++
				case '}':
					depth--
				}
				if depth > 0 {
					k++
				}
			}
			b.WriteString(StripSnippet(string(r[j:min(k, len(r))])))
			i = k
		default:
			b.WriteRune(r[i])
		}
	}
	return b.String()
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
