// Package provider defines the single LLM boundary of the agent runtime
// (ADR-0003): a narrow streaming interface with two implementations — a
// scripted Mock for deterministic offline tests and an Anthropic Messages
// API adapter. Everything above this package (tools, bus, UI) speaks only
// the neutral vocabulary defined here.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
)

// Role identifies who authored a Message.
type Role string

// Message authors.
const (
	User      Role = "user"
	Assistant Role = "assistant"
)

// Block is one piece of message content: text, a tool invocation issued
// by the model, or a tool result fed back by the runtime.
type Block interface{ block() }

// Text is literal message content.
type Text struct{ Value string }

// ToolUse is the model requesting a tool call.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is the runtime answering a ToolUse.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

func (Text) block()       {}
func (ToolUse) block()    {}
func (ToolResult) block() {}

// Message is one conversation turn composed of ordered blocks.
type Message struct {
	Role   Role
	Blocks []Block
}

// ToolDef describes an callable tool to the provider: a name, human
// description, and JSON-schema object describing parameters.
type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Request is one completion request. System is the flattened system
// prompt; Messages alternate user/assistant (tool results ride inside
// user turns, matching the wire format).
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

// Kind classifies stream events.
type Kind string

// Stream event kinds.
const (
	EventText    Kind = "text"     // incremental Text payload
	EventToolUse Kind = "tool_use" // complete tool call (input assembled)
	EventStop    Kind = "stop"     // terminal; StopReason set
	EventError   Kind = "error"    // fatal for this stream; Err set
)

// Stop reasons (Anthropic vocabulary; Mock reuses it).
const (
	StopEndTurn string = "end_turn"
	StopToolUse string = "tool_use"
)

// Event is one streamed increment. The channel closes after EventStop,
// after EventError, or when the context is cancelled — consumers select
// on channel receive and ctx.Done().
type Event struct {
	Kind       Kind
	Text       string          // EventText
	ToolUseID  string          // EventToolUse
	ToolName   string          // EventToolUse
	Input      json.RawMessage // EventToolUse
	StopReason string          // EventStop
	Err        error           // EventError
}

// Provider streams completions. Implementations must be safe for
// concurrent use and must not block indefinitely on cancelled contexts.
type Provider interface {
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

func errf(format string, args ...any) error {
	return fmt.Errorf("provider: "+format, args...)
}
