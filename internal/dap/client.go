// Package dap is a minimal Debug Adapter Protocol client (F-039): the
// wire codec and request/response correlation (this file) plus a Session
// that drives the launch handshake and tracks stop state (session.go).
// It mirrors internal/lsp's shape: a Content-Length framed JSON stream,
// one read loop, calls that block until their response or a timeout.
package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Event is an adapter-initiated notification.
type Event struct {
	Name string
	Body json.RawMessage
}

type message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	Arguments  any             `json:"arguments,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success,omitempty"`
	Message    string          `json:"message,omitempty"`
	Event      string          `json:"event,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

type response struct {
	ok   bool
	msg  string
	body json.RawMessage
}

// ErrClosed reports a call on (or interrupted by) a closed connection.
var ErrClosed = errors.New("dap: connection closed")

// Client is one adapter connection.
type Client struct {
	conn io.ReadWriteCloser
	rd   *bufio.Reader

	wmu     sync.Mutex
	mu      sync.Mutex
	seq     int
	pending map[int]chan response
	closed  bool

	events chan Event
}

// New starts the read loop on conn.
func New(conn io.ReadWriteCloser) *Client {
	c := &Client{
		conn:    conn,
		rd:      bufio.NewReader(conn),
		pending: map[int]chan response{},
		events:  make(chan Event, 256),
	}
	go c.readLoop()
	return c
}

// Events delivers adapter events in order; it closes when the
// connection ends.
func (c *Client) Events() <-chan Event { return c.events }

// Close shuts the connection; pending calls fail with ErrClosed.
func (c *Client) Close() error {
	c.fail()
	return c.conn.Close()
}

func (c *Client) fail() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

func (c *Client) readLoop() {
	defer close(c.events)
	defer c.fail()
	for {
		payload, err := readFrame(c.rd)
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(payload, &m) != nil {
			continue
		}
		switch m.Type {
		case "response":
			c.mu.Lock()
			ch := c.pending[m.RequestSeq]
			delete(c.pending, m.RequestSeq)
			c.mu.Unlock()
			if ch != nil {
				ch <- response{ok: m.Success, msg: m.Message, body: m.Body}
			}
		case "event":
			select {
			case c.events <- Event{Name: m.Event, Body: m.Body}:
			default: // a stalled consumer must not wedge the read loop
			}
		}
	}
}

// Pending is an in-flight request.
type Pending struct {
	c   *Client
	cmd string
	ch  chan response
}

// Send issues a request without waiting for its response.
func (c *Client) Send(cmd string, args any) (*Pending, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.seq++
	id := c.seq
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	data, err := json.Marshal(message{Seq: id, Type: "request", Command: cmd, Arguments: args})
	if err != nil {
		return nil, err
	}
	c.wmu.Lock()
	_, werr := fmt.Fprintf(c.conn, "Content-Length: %d\r\n\r\n%s", len(data), data)
	c.wmu.Unlock()
	if werr != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, werr
	}
	return &Pending{c: c, cmd: cmd, ch: ch}, nil
}

// Wait blocks for the response, decoding the body into out (may be nil).
func (p *Pending) Wait(ctx context.Context, out any) error {
	select {
	case r, ok := <-p.ch:
		if !ok {
			return ErrClosed
		}
		if !r.ok {
			msg := nonEmpty(r.msg, "request failed")
			if detail := errorDetail(r.body); detail != "" && !strings.Contains(msg, detail) {
				msg += ": " + detail
			}
			return fmt.Errorf("dap: %s: %s", p.cmd, msg)
		}
		if out != nil && len(r.body) > 0 {
			if err := json.Unmarshal(r.body, out); err != nil {
				return fmt.Errorf("dap: %s response: %w", p.cmd, err)
			}
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("dap: %s: %w", p.cmd, ctx.Err())
	}
}

// Call sends a request and waits (bounded by ctx) for the response.
func (c *Client) Call(ctx context.Context, cmd string, args, out any) error {
	p, err := c.Send(cmd, args)
	if err != nil {
		return err
	}
	return p.Wait(ctx, out)
}

// errorDetail extracts the human reason adapters put in an error
// response's body (`{"error":{"format":"…"}}`); the short `message` field
// is often only a category such as "Failed to launch".
func errorDetail(body json.RawMessage) string {
	if len(body) == 0 {
		return ""
	}
	var b struct {
		Error struct {
			Format string `json:"format"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &b) != nil {
		return ""
	}
	return strings.TrimSpace(b.Error.Format)
}

func nonEmpty(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
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
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &length); err != nil {
				return nil, fmt.Errorf("dap: bad Content-Length %q", v)
			}
		}
	}
	if length < 0 {
		return nil, errors.New("dap: frame without Content-Length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// withTimeout derives a bounded context for one call.
func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
