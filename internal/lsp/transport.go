// Package lsp is DHI's minimal Language Server Protocol client
// (F-002 §7): JSON-RPC 2.0 over stdio with just the flows the editor
// needs — initialize handshake, full-text didOpen/didChange, streamed
// publishDiagnostics, and completion requests. Servers resolve through
// the hermetic toolchain shim dir (ADR-0005); tests drive an in-process
// fake server over net.Pipe.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// writeFrame emits one LSP-framed message.
func writeFrame(w io.Writer, payload []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readFrame reads one framed message; io.EOF at a clean boundary ends.
func readFrame(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("Content-Length:"):]))
			if err != nil {
				return nil, fmt.Errorf("lsp: bad content-length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("lsp: missing content-length header")
	}
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// rpcMessage is the union shape of JSON-RPC 2.0.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func encodeRequest(id int64, method string, params any) ([]byte, error) {
	p, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: p})
}

func encodeNotification(method string, params any) ([]byte, error) {
	p, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rpcMessage{JSONRPC: "2.0", Method: method, Params: p})
}

func ctxErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
