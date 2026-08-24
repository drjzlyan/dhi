// Package tools defines the agent tool seam: a registry of callable
// capabilities plus the four builtins (read/write/list/search) that give
// models access to workspace files exclusively as namespaced vpaths.
// Every filesystem touch goes through sandbox.Guard first (ADR-0006);
// Ask decisions are parked as approvals instead of executing.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Call is one tool invocation issued by a model.
type Call struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Result answers a Call; Content feeds back into the conversation.
type Result struct {
	CallID  string
	Content string
	IsError bool
}

// Tool is one callable capability. Exec must be safe for concurrent use
// and must honor ctx cancellation.
type Tool interface {
	Def() Def
	Exec(ctx context.Context, input json.RawMessage) (string, error)
}

// Def describes a tool to providers (mirrors provider.ToolDef so this
// package does not depend on any provider implementation details).
type Def struct {
	Name        string
	Description string
	InputSchema string
}

// Registry holds tools by name; registration order is irrelevant because
// lookups and definition listings are keyed/sorted deterministically.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds t; duplicate names are rejected.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Def().Name
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tools: duplicate tool %q", name)
	}
	r.tools[name] = t
	return nil
}

// Get looks a tool up by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names lists registered tool names sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sortStrings(out)
	return out
}

// Defs renders definitions for the given allowlist, preserving allowlist
// order so prompts stay stable. Unknown names are skipped (manifest
// validation already guarantees well-formed references).
func (r *Registry) Defs(allow []string) []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Def, 0, len(allow))
	for _, name := range allow {
		if t, ok := r.tools[name]; ok {
			out = append(out, t.Def())
		}
	}
	return out
}

// Call routes one invocation, converting panics/errors into error
// results so a misbehaving tool can never kill a turn.
func (r *Registry) Call(ctx context.Context, c Call) Result {
	r.mu.RLock()
	t, ok := r.tools[c.Name]
	r.mu.RUnlock()
	if !ok {
		return Result{CallID: c.ID, Content: fmt.Sprintf("unknown tool %q", c.Name), IsError: true}
	}
	content, err := func() (s string, err error) {
		defer func() {
			if p := recover(); p != nil {
				s, err = "", fmt.Errorf("tool %q panicked: %v", c.Name, p)
			}
		}()
		return t.Exec(ctx, c.Input)
	}()
	if err != nil {
		return Result{CallID: c.ID, Content: err.Error(), IsError: true}
	}
	return Result{CallID: c.ID, Content: content}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
