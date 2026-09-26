// Package dhitools is the IDE-as-tools surface for agents (F-028,
// ADR-0017): the domain layer rendered as MCP tools. Every tool is
// gated by the agent manifest's `tools` allowlist (a tool absent from
// the list is absent from tools/list — the agent sees what it owns),
// and mutating tools cross tools.Approvals.Ask so the human's y/n
// gates every effect, exactly as the dhi-action bridge did.
//
// The handlers run IN the DHI process: the runtime serves them over a
// per-turn loopback endpoint, so approvals and stores are the same
// instances the TUI uses. Filesystem work stays with the sandboxed
// CLI's own tools; git stays CLI-native.
package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Deps carries the stores one agent's tool surface operates on. The
// instances are the SAME ones the TUI opened (single process, single
// truth). Zero-value store refs simply omit their tools.
type Deps struct {
	Agent     *manifest.Agent // allowlist + agent id
	Tasks     *tasks.Store
	KB        knowledge.KnowledgeStore
	Memory    *memory.Store
	Bus       *bus.Bus
	WS        *workspace.Workspace
	Search    search.Searcher
	Approvals *tools.Approvals
	Git       GitCLI               // git CLI for git_diff; nil refuses the diff tool
	Identity  gitcore.IdentityFunc // commit identity; nil refuses git_commit
	Sessions  *ideation.Store      // ideation read tools; nil omits them
	Run       CommandRunner        // allowlisted command runner; nil refuses `run`

	// Channel/Thread are the trigger context: channel_read/post
	// default to the thread the turn started from.
	Channel string
	Thread  int64

	// Workdir is the turn's working directory (the task worktree when the
	// trigger is bound to one, else the workspace root). The git tools
	// operate here.
	Workdir string
}

// tool is one served tool's declaration + handler. Args parse in two
// phases: parse runs BEFORE the approval gate so a malformed call is
// refused without parking a pointless human prompt (F-028: the y/n is
// for real effects, never for schema errors); exec runs after.
type tool struct {
	info   mcp.ToolInfo
	mutate bool // crosses approvals
	parse  func(raw json.RawMessage) (any, error)
	exec   func(ctx context.Context, decoded any) (string, error)
}

// servedSlugs is the F-028 IDE-tools surface. The runtime turns serving
// on when an agent's allowlist intersects it; doctor reports capability
// from the same list (one source of truth).
var servedSlugs = []string{
	"read", "write", "patch", "list", "glob",
	"git_status", "git_log", "git_branch", "git_diff", "git_commit",
	"ideation_list", "ideation_read",
	"run", "ask_human",
	"task_list", "task_create", "task_status", "task_assign",
	"kb_search", "kb_contribute",
	"memory_append", "memory_read_notes", "memory_write_notes",
	"channel_read", "channel_post",
	"workspace_search",
}

// ServedTools returns the F-028 tool slugs this surface can serve.
func ServedTools() []string {
	out := make([]string, len(servedSlugs))
	copy(out, servedSlugs)
	return out
}

// Serves reports whether name is one of the served IDE tools.
func Serves(name string) bool {
	for _, s := range servedSlugs {
		if s == name {
			return true
		}
	}
	return false
}

// Handler builds the agent's MCP tool surface: only allowlisted tools
// appear. An agent with no served tools yields an empty surface (the
// runtime skips serving entirely in that case).
func (d Deps) Handler() mcp.Handler {
	allow := map[string]bool{}
	for _, t := range d.Agent.Tools {
		allow[t] = true
	}
	var all []tool
	all = append(all, d.fsTools()...)
	all = append(all, d.gitTools()...)
	all = append(all, d.ideationTools()...)
	all = append(all, d.miscTools()...)
	all = append(all, d.taskTools()...)
	all = append(all, d.kbTools()...)
	all = append(all, d.memoryTools()...)
	all = append(all, d.channelTools()...)
	all = append(all, d.searchTools()...)

	var served []tool
	for _, t := range all {
		if allow[t.info.Name] {
			served = append(served, t)
		}
	}
	return &handler{deps: d, tools: served}
}

// handler implements mcp.Handler.
type handler struct {
	deps  Deps
	tools []tool
}

func (h *handler) ProtocolVersion() string { return mcp.ProtocolVersion }

func (h *handler) ServerInfo() (string, string) {
	return "dhi", "1"
}

func (h *handler) Tools() []mcp.ToolInfo {
	out := make([]mcp.ToolInfo, 0, len(h.tools))
	for _, t := range h.tools {
		out = append(out, t.info)
	}
	return out
}

func (h *handler) CallTool(ctx context.Context, name string, raw json.RawMessage) (string, bool, error) {
	for _, t := range h.tools {
		if t.info.Name != name {
			continue
		}
		decoded, perr := t.parse(raw)
		if perr != nil {
			return perr.Error(), true, nil // schema refusal, no approval spent
		}
		if t.mutate {
			if h.deps.Approvals == nil {
				return "tool " + name + " unavailable: approvals queue not configured", true, nil
			}
			// The human's y/n: blocks until resolved or ctx ends —
			// the TUI answers through the same queue as every other
			// gated op (F-028 Part A). A denial keeps its named
			// reason as the tool's content.
			if err := h.deps.Approvals.Ask(ctx, h.deps.Agent.ID, sandbox.OpExec,
				"tool "+name, "agent-requested DHI tool"); err != nil {
				return err.Error(), true, nil
			}
		}
		out, err := t.exec(ctx, decoded)
		if err != nil {
			return err.Error(), true, nil // tool-level failure, agent-visible
		}
		return out, false, nil
	}
	return "", false, fmt.Errorf("unknown tool %q", name)
}

// args decodes strict JSON arguments into out (unknown keys refused —
// the strict-data rule applies to tool args too).
func args(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("arguments: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// task tools
// ---------------------------------------------------------------------------

func (d Deps) taskTools() []tool {
	var out []tool
	if d.Tasks == nil {
		return out
	}
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "task_list",
			Description: "List the workspace task cards (kanban). Returns slug, status, title, assignee per card.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) { return nil, nil },
		exec: func(ctx context.Context, _ any) (string, error) {
			cards := d.Tasks.List()
			sort.Slice(cards, func(i, j int) bool { return cards[i].Slug < cards[j].Slug })
			var b strings.Builder
			for _, tk := range cards {
				fmt.Fprintf(&b, "%s [%s] %s (assignee: %s)\n",
					tk.Slug, tk.Status, tk.Title, orNone(tk.Assignee))
			}
			if b.Len() == 0 {
				return "(no tasks)\n", nil
			}
			return b.String(), nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "task_create",
			Description: "Create a task card. Args: {\"slug\": \"short-kebab-id\", \"title\": \"what and why\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["slug","title"],"properties":{"slug":{"type":"string"},"title":{"type":"string"}},"additionalProperties":false}`),
		},
		mutate: true,
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Slug  string `json:"slug"`
				Title string `json:"title"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Slug  string `json:"slug"`
				Title string `json:"title"`
			})
			if err := d.Tasks.Create(strings.TrimSpace(a.Slug), strings.TrimSpace(a.Title), "", ""); err != nil {
				return "", err
			}
			return "created task " + a.Slug, nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "task_status",
			Description: "Move a task card to another lane. Args: {\"slug\": \"...\", \"status\": \"backlog|active|in-review|done\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["slug","status"],"properties":{"slug":{"type":"string"},"status":{"type":"string"}},"additionalProperties":false}`),
		},
		mutate: true,
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Slug   string `json:"slug"`
				Status string `json:"status"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Slug   string `json:"slug"`
				Status string `json:"status"`
			})
			if err := d.Tasks.SetStatus(strings.TrimSpace(a.Slug), tasks.Status(a.Status)); err != nil {
				return "", err
			}
			return "task " + a.Slug + " → " + a.Status, nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "task_assign",
			Description: "Assign a task card to an agent id (or clear with \"\"). Args: {\"slug\": \"...\", \"assignee\": \"...\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["slug","assignee"],"properties":{"slug":{"type":"string"},"assignee":{"type":"string"}},"additionalProperties":false}`),
		},
		mutate: true,
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Slug     string `json:"slug"`
				Assignee string `json:"assignee"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Slug     string `json:"slug"`
				Assignee string `json:"assignee"`
			})
			if err := d.Tasks.Assign(strings.TrimSpace(a.Slug), strings.TrimSpace(a.Assignee)); err != nil {
				return "", err
			}
			return "task " + a.Slug + " assigned to " + orNone(a.Assignee), nil
		},
	})
	return out
}

// ---------------------------------------------------------------------------
// knowledge tools
// ---------------------------------------------------------------------------

func (d Deps) kbTools() []tool {
	var out []tool
	if d.KB == nil {
		return out
	}
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "kb_search",
			Description: "Search the workspace knowledge base. Args: {\"query\": \"what to look for\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Query string `json:"query"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Query string `json:"query"`
			})
			hits, err := d.KB.Search(ctx, a.Query, 5)
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "(no knowledge base hits)\n", nil
			}
			var b strings.Builder
			for _, h := range hits {
				fmt.Fprintf(&b, "- %s: %s\n", h.Entry.Title, h.Snippet)
			}
			return b.String(), nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "kb_contribute",
			Description: "Contribute a knowledge base entry (lands in the review queue; the human approves publication). Args: {\"title\": \"...\", \"body\": \"...\", \"tags\": [\"...\"]}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["title","body"],"properties":{"title":{"type":"string"},"body":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`),
		},
		mutate: true,
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Title string   `json:"title"`
				Body  string   `json:"body"`
				Tags  []string `json:"tags"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Title string   `json:"title"`
				Body  string   `json:"body"`
				Tags  []string `json:"tags"`
			})
			status, id, err := d.KB.Contribute(knowledge.Contribution{
				Title: a.Title, Body: a.Body, Tags: a.Tags, Author: d.Agent.ID,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("contributed %s (%s — the human reviews publication)", id, status), nil
		},
	})
	return out
}

// ---------------------------------------------------------------------------
// memory tools (the agent's own notebook — no approvals by design:
// private state, invisible to collaborators)
// ---------------------------------------------------------------------------

func (d Deps) memoryTools() []tool {
	var out []tool
	if d.Memory == nil {
		return out
	}
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "memory_append",
			Description: "Append a durable lesson to your journal (survives turns; keep it short). Args: {\"text\": \"...\", \"kind\": \"lesson|turn|preference\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"kind":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Text string `json:"text"`
				Kind string `json:"kind"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Text string `json:"text"`
				Kind string `json:"kind"`
			})
			if strings.TrimSpace(a.Kind) == "" {
				a.Kind = "turn"
			}
			if err := d.Memory.Append(d.Agent.ID, a.Kind, a.Text); err != nil {
				return "", err
			}
			return "journaled", nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "memory_read_notes",
			Description: "Read your persistent notes file.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) { return nil, nil },
		exec: func(ctx context.Context, _ any) (string, error) {
			notes, err := d.Memory.ReadNotes(d.Agent.ID)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(notes) == "" {
				return "(notes empty)\n", nil
			}
			return notes, nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "memory_write_notes",
			Description: "Replace your persistent notes file wholesale. Args: {\"content\": \"full new notes text\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["content"],"properties":{"content":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Content string `json:"content"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Content string `json:"content"`
			})
			if err := d.Memory.WriteNotes(d.Agent.ID, a.Content); err != nil {
				return "", err
			}
			return "notes written", nil
		},
	})
	return out
}

// ---------------------------------------------------------------------------
// channel tools
// ---------------------------------------------------------------------------

func (d Deps) channelTools() []tool {
	var out []tool
	if d.Bus == nil {
		return out
	}
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "channel_read",
			Description: "Read recent messages from a channel (default: the thread that started this turn). Args: {\"channel\": \"#name or dm:<agent>\" (optional)}.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"channel":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Channel string `json:"channel"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Channel string `json:"channel"`
			})
			ch := a.Channel
			if ch == "" {
				ch = d.Channel
			}
			history := d.Bus.History(ch, 0)
			if len(history) > 30 {
				history = history[len(history)-30:]
			}
			if len(history) == 0 {
				return "(no messages in " + ch + ")\n", nil
			}
			var b strings.Builder
			for _, m := range history {
				who := m.Author
				if who == bus.Human {
					who = "you (the human)"
				}
				fmt.Fprintf(&b, "%s: %s\n", who, m.Text)
			}
			return b.String(), nil
		},
	})
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "channel_post",
			Description: "Post a message to a channel (default: this turn's thread). Args: {\"text\": \"...\", \"channel\": \"...\" (optional)}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"channel":{"type":"string"}},"additionalProperties":false}`),
		},
		mutate: true,
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Text    string `json:"text"`
				Channel string `json:"channel"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Text    string `json:"text"`
				Channel string `json:"channel"`
			})
			ch := a.Channel
			if ch == "" {
				ch = d.Channel
			}
			posted, err := d.Bus.Post(bus.Message{
				Channel: ch, Thread: d.Thread, Author: d.Agent.ID, Text: a.Text,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("posted %s to %s", itoa64(posted.ID), ch), nil
		},
	})
	return out
}

// ---------------------------------------------------------------------------
// workspace search
// ---------------------------------------------------------------------------

func (d Deps) searchTools() []tool {
	var out []tool
	if d.Search == nil || d.WS == nil {
		return out
	}
	roots := make([]string, 0, len(d.WS.Members())+1)
	for _, m := range d.WS.Members() {
		roots = append(roots, m.Path)
	}
	roots = append(roots, d.WS.Root+"/.dhi")
	out = append(out, tool{
		info: mcp.ToolInfo{
			Name:        "workspace_search",
			Description: "Search member repos and .dhi for a text pattern (ripgrep). Args: {\"query\": \"pattern\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Query string `json:"query"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			return a, nil
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			a := dec.(struct {
				Query string `json:"query"`
			})
			hits, err := d.Search.Search(ctx, a.Query, roots)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			n := 0
			for h := range hits {
				if n >= 30 {
					fmt.Fprintf(&b, "(more hits omitted)\n")
					break
				}
				fmt.Fprintf(&b, "%s:%d: %s\n", shortenPath(h.Path, d.WS.Root), h.Line, h.Text)
				n++
			}
			if n == 0 {
				return "(no matches)\n", nil
			}
			return b.String(), nil
		},
	})
	return out
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "none"
	}
	return s
}

func itoa64(v int64) string {
	return fmt.Sprintf("%d", v)
}

// shortenPath renders member-relative paths the way grounding teaches.
func shortenPath(p, root string) string {
	rel, err := filepath.Rel(root, p)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}
