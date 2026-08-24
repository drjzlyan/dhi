package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the production Messages API endpoint.
const DefaultBaseURL = "https://api.anthropic.com"

// APIVersion pins the wire protocol revision.
const APIVersion = "2023-06-01"

// Anthropic calls the Anthropic Messages API over plain HTTP with SSE
// streaming. No SDK dependency (F-007 decision 1); the adapter is small,
// explicit, and testable against httptest fixture servers.
type Anthropic struct {
	BaseURL string       // default DefaultBaseURL
	APIKey  string       // resolved by the caller from the agent's env var
	HTTP    *http.Client // default 60s timeout client
	Version string       // anthropic-version header override for tests
}

// NewAnthropic fills defaults.
func NewAnthropic(baseURL, apiKey string) *Anthropic {
	return &Anthropic{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

type wireRequest struct {
	Model     string     `json:"model"`
	MaxTokens int        `json:"max_tokens"`
	System    string     `json:"system,omitempty"`
	Messages  []wireMsg  `json:"messages"`
	Tools     []wireTool `json:"tools,omitempty"`
	Stream    bool       `json:"stream"`
}

type wireMsg struct {
	Role    Role        `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireBlock struct {
	Type string `json:"type"`

	// text / tool_result
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func toWire(req Request) wireRequest {
	max := req.MaxTokens
	if max <= 0 {
		max = 4096
	}
	w := wireRequest{
		Model:     req.Model,
		MaxTokens: max,
		System:    req.System,
		Messages:  make([]wireMsg, 0, len(req.Messages)),
		Stream:    true,
	}
	for _, m := range req.Messages {
		wm := wireMsg{Role: m.Role, Content: make([]wireBlock, 0, len(m.Blocks))}
		for _, b := range m.Blocks {
			switch v := b.(type) {
			case Text:
				wm.Content = append(wm.Content, wireBlock{Type: "text", Text: v.Value})
			case ToolUse:
				wm.Content = append(wm.Content, wireBlock{Type: "tool_use", ID: v.ID, Name: v.Name, Input: v.Input})
			case ToolResult:
				wm.Content = append(wm.Content, wireBlock{Type: "tool_result", ToolUseID: v.ToolUseID, Text: v.Content, IsError: v.IsError})
			}
		}
		w.Messages = append(w.Messages, wm)
	}
	for _, t := range req.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		w.Tools = append(w.Tools, wireTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return w
}

// Stream issues the request and returns a channel of parsed events. Setup
// failures (bad URL, non-2xx response) return as an immediate error;
// mid-stream failures arrive as EventError before close.
func (a *Anthropic) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	base := a.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	body, err := json.Marshal(toWire(req))
	if err != nil {
		return nil, errf("anthropic: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(base, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, errf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", a.version())
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, errf("anthropic: post: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, errf("anthropic: status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		streamSSE(ctx, resp.Body, func(ev Event) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()
	return out, nil
}

func (a *Anthropic) version() string {
	if a.Version != "" {
		return a.Version
	}
	return APIVersion
}

// sseEvent is one decoded "data:" payload of the SSE stream. The delta
// object differs per event type, so it is kept raw and re-decoded by the
// handler for that type.
type sseEvent struct {
	Type  string `json:"type"`
	Index *int   `json:"index"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta json.RawMessage `json:"delta"`
}

type msgDelta struct {
	StopReason string `json:"stop_reason"`
}

type blockDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	PartialJSON string `json:"partial_json"`
}

// streamSSE reads Anthropic SSE framing and invokes emit per neutral
// event; emit returning false stops consumption (context cancelled).
func streamSSE(ctx context.Context, r io.Reader, emit func(Event) bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	var (
		stopReason string
		errored    bool
		tools      map[int]*toolAccum // content block index → accumulating tool call
	)
	emitErr := func(err error) bool {
		errored = true
		return emit(Event{Kind: EventError, Err: err})
	}
	drain := func(ev sseEvent) bool {
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" && ev.Index != nil {
				if tools == nil {
					tools = map[int]*toolAccum{}
				}
				tools[*ev.Index] = &toolAccum{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
			}
		case "content_block_delta":
			if len(ev.Delta) == 0 || ev.Index == nil {
				return true
			}
			var d blockDelta
			if err := json.Unmarshal(ev.Delta, &d); err != nil {
				return emitErr(errf("anthropic: bad block delta: %w", err))
			}
			switch d.Type {
			case "text_delta":
				if !emit(Event{Kind: EventText, Text: d.Text}) {
					return false
				}
			case "input_json_delta":
				if acc := tools[*ev.Index]; acc != nil {
					acc.buf.WriteString(d.PartialJSON)
				}
			}
		case "content_block_stop":
			if ev.Index != nil {
				if acc := tools[*ev.Index]; acc != nil {
					input := acc.finish()
					ok := emit(Event{Kind: EventToolUse, ToolUseID: acc.id, ToolName: acc.name, Input: input})
					delete(tools, *ev.Index)
					return ok
				}
			}
		case "message_delta":
			var d msgDelta
			if len(ev.Delta) > 0 && json.Unmarshal(ev.Delta, &d) == nil {
				stopReason = d.StopReason
			}
		case "message_stop":
			return emit(Event{Kind: EventStop, StopReason: stopReason})
		case "error":
			if ev.Error != nil {
				return emitErr(errf("anthropic: %s: %s", ev.Error.Type, ev.Error.Message))
			}
			return emitErr(errf("anthropic: stream error"))
		}
		return true
	}

	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // event:/ping/comment lines carry nothing we need
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var ev sseEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			if !emitErr(errf("anthropic: bad sse data: %w", err)) {
				return
			}
			continue
		}
		if !drain(ev) {
			return
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		emitErr(errf("anthropic: read stream: %w", err))
		return
	}
	// Scanner ended without message_stop or error (server hang-up):
	// still emit a terminal event so consumers never wait forever.
	select {
	case <-ctx.Done():
	default:
		if !errored {
			emit(Event{Kind: EventStop, StopReason: stopReason})
		}
	}
}

// toolAccumulator gathers input_json_delta fragments for one tool call.
type toolAccum struct {
	id   string
	name string
	buf  strings.Builder
}

func (t *toolAccum) finish() json.RawMessage {
	s := strings.TrimSpace(t.buf.String())
	if s == "" {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(s)
}
