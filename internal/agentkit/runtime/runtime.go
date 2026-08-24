// Package runtime is the agent turn engine: it owns the roster, routes
// bus mentions to the right agent, assembles conversations from channel
// history, drives provider streams through tool round-trips inside the
// sandbox, and posts replies back to the bus. UIs subscribe to the bus;
// they never talk to providers directly.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/provider"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/mcp"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// maxToolRounds bounds a single turn's tool loop.
const maxToolRounds = 8

// historyWindow caps how much channel context feeds a prompt.
const historyWindow = 50

// MCPClient is a connected tool server: discovery plus invocation
// (satisfied by *mcp.Stdio / *mcp.HTTP).
type MCPClient interface {
	Tools(ctx context.Context) ([]mcp.ToolInfo, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (content string, isError bool, err error)
}

// Config wires the runtime's seams. Provider serves agents without an
// override; MCPClients must be pre-connected by the embedder (transports
// outlive turns).
type Config struct {
	WS         *workspace.Workspace
	Bus        *bus.Bus
	Approvals  *tools.Approvals
	Searcher   search.Searcher
	Provider   provider.Provider
	Providers  map[string]provider.Provider // agent id → override
	MCPClients map[string]MCPClient         // server name → connected client
	// Org supplies team membership for layered coding standards; nil
	// disables team layers.
	Org *org.Org
	// Standards injects layered coding instructions into every turn's
	// system prompt (built-ins apply even without a document).
	Standards bool
}

// Runtime manages rostered agents and executes their turns.
type Runtime struct {
	cfg    Config
	mu     sync.Mutex
	agents map[string]*entry
	roster chan struct{} // pinged after every Reload
}

type entry struct {
	m      *manifest.Agent
	p      provider.Provider
	reg    *tools.Registry
	turnMu sync.Mutex // one turn at a time per agent
}

// New builds per-agent registries from the roster. Agents whose manifest
// fails are skipped with an error return only if none load.
func New(cfg Config, roster []*manifest.Agent) (*Runtime, error) {
	r := &Runtime{cfg: cfg, agents: map[string]*entry{}, roster: make(chan struct{}, 1)}
	if len(roster) == 0 {
		return nil, fmt.Errorf("runtime: empty roster")
	}
	jailRoots := make([]string, 0, len(cfg.WS.Members())+1)
	for _, m := range cfg.WS.Members() {
		jailRoots = append(jailRoots, m.Path)
	}
	for _, m := range roster {
		e, err := r.buildEntry(m, jailRoots)
		if err != nil {
			return nil, err
		}
		r.agents[m.ID] = e
	}
	return r, nil
}

// buildEntry wires one agent's provider, jail, and tool registry.
func (r *Runtime) buildEntry(m *manifest.Agent, jailRoots []string) (*entry, error) {
	e := &entry{m: m}
	if p, ok := r.cfg.Providers[m.ID]; ok {
		e.p = p
	} else {
		e.p = r.cfg.Provider
	}
	if e.p == nil {
		return nil, fmt.Errorf("runtime: %s has no provider", m.ID)
	}
	var policy *sandbox.Policy
	if m.Policy() != nil {
		policy = m.Policy()
	} else {
		policy = &sandbox.Policy{} // deny-all default
	}
	jail, err := sandbox.NewJail(jailRoots...)
	if err != nil {
		return nil, fmt.Errorf("runtime: jail: %w", err)
	}
	deps := tools.Deps{
		WS:        r.cfg.WS,
		Guard:     sandbox.NewGuard(jail, policy),
		Approvals: r.cfg.Approvals,
		Searcher:  r.cfg.Searcher,
		AgentID:   m.ID,
	}
	e.reg = tools.New()
	for _, t := range tools.Builtins(deps) {
		if err := e.reg.Register(t); err != nil {
			return nil, fmt.Errorf("runtime: %s: %w", m.ID, err)
		}
	}
	gate := tools.PolicyGate(policy, r.cfg.Approvals, m.ID)
	for server, client := range r.cfg.MCPClients {
		infos, err := client.Tools(context.Background())
		if err != nil {
			return nil, fmt.Errorf("runtime: mcp server %q: %w", server, err)
		}
		remotes := make([]tools.RemoteInfo, 0, len(infos))
		for _, i := range infos {
			remotes = append(remotes, tools.RemoteInfo{Name: i.Name, Description: i.Description, Schema: i.InputSchema})
		}
		for _, t := range tools.RemoteTools(server, gate, client, remotes) {
			if !allowed(m.Tools, t.Def().Name) {
				continue
			}
			if err := e.reg.Register(t); err != nil {
				return nil, fmt.Errorf("runtime: %s: %w", m.ID, err)
			}
		}
	}
	return e, nil
}

// Changes pings once after every successful Reload so UIs can refresh
// rosters (best-effort delivery).
func (r *Runtime) Changes() <-chan struct{} { return r.roster }

// Reload swaps the active roster atomically: entries are rebuilt first,
// then the map is replaced under lock. Turns already running against old
// entries finish untouched (they hold their own turnMu); new turns bind
// to the new entries. An empty roster clears the crew.
func (r *Runtime) Reload(roster []*manifest.Agent) error {
	jailRoots := make([]string, 0, len(r.cfg.WS.Members())+1)
	for _, m := range r.cfg.WS.Members() {
		jailRoots = append(jailRoots, m.Path)
	}
	next := make(map[string]*entry, len(roster))
	for _, m := range roster {
		e, err := r.buildEntry(m, jailRoots)
		if err != nil {
			return fmt.Errorf("runtime: reload aborted; previous roster kept: %w", err)
		}
		next[m.ID] = e
	}
	r.mu.Lock()
	r.agents = next
	r.mu.Unlock()
	select {
	case r.roster <- struct{}{}:
	default:
	}
	return nil
}

func allowed(allow []string, name string) bool {
	for _, a := range allow {
		if a == name {
			return true
		}
	}
	return false
}

// AgentIDs lists rostered agents sorted.
func (r *Runtime) AgentIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.agents))
	for id := range r.agents {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

// Bus exposes the message bus for UI subscribers.
func (r *Runtime) Bus() *bus.Bus { return r.cfg.Bus }

// Approvals exposes the shared approval queue for UIs.
func (r *Runtime) Approvals() *tools.Approvals { return r.cfg.Approvals }

// Handle processes one inbound message: any mentioned rostered agent
// (or the sole DM addressee) runs a turn in its own goroutine. It returns
// immediately after dispatching.
func (r *Runtime) Handle(ctx context.Context, msg bus.Message) {
	for _, id := range r.targets(msg) {
		go func(id string) {
			_ = r.Turn(ctx, id, msg)
		}(id)
	}
}

// targets resolves which agents a message addresses.
func (r *Runtime) targets(msg bus.Message) []string {
	if strings.HasPrefix(msg.Channel, "dm:") {
		id := strings.TrimPrefix(msg.Channel, "dm:")
		if _, ok := r.agents[id]; ok && msg.Author != id {
			return []string{id}
		}
		return nil
	}
	var out []string
	for _, id := range bus.Mentions(msg.Text) {
		if _, ok := r.agents[id]; ok && id != msg.Author {
			out = append(out, id)
		}
	}
	return out
}

// Turn executes one full conversational turn for agentID in response to
// trigger: history → prompt → streamed completion → tool round-trips →
// reply posted to the trigger's channel/thread.
func (r *Runtime) Turn(ctx context.Context, agentID string, trigger bus.Message) error {
	r.mu.Lock()
	e, ok := r.agents[agentID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("runtime: unknown agent %q", agentID)
	}
	e.turnMu.Lock()
	defer e.turnMu.Unlock()

	req := r.prompt(e, trigger)

	var reply strings.Builder
	for round := 0; round < maxToolRounds; round++ {
		ch, err := e.p.Stream(ctx, req)
		if err != nil {
			return fmt.Errorf("runtime: %s: stream: %w", agentID, err)
		}
		var calls []tools.Call
		stop := ""
		for ev := range ch {
			switch ev.Kind {
			case provider.EventText:
				reply.WriteString(ev.Text)
			case provider.EventToolUse:
				calls = append(calls, tools.Call{ID: ev.ToolUseID, Name: ev.ToolName, Input: ev.Input})
			case provider.EventStop:
				stop = ev.StopReason
			case provider.EventError:
				return fmt.Errorf("runtime: %s: %w", agentID, ev.Err)
			}
		}
		if len(calls) == 0 || stop != provider.StopToolUse {
			break // plain end: fall through to reply
		}

		// Record the assistant tool-use turn, execute, feed results back.
		asst := provider.Message{Role: provider.Assistant, Blocks: []provider.Block{
			provider.Text{Value: reply.String()},
		}}
		for _, c := range calls {
			asst.Blocks = append(asst.Blocks, provider.ToolUse{ID: c.ID, Name: c.Name, Input: c.Input})
		}
		req.Messages = append(req.Messages, asst)

		results := provider.Message{Role: provider.User}
		for _, c := range calls {
			res := e.reg.Call(ctx, c)
			results.Blocks = append(results.Blocks, provider.ToolResult{
				ToolUseID: res.CallID,
				Content:   res.Content,
				IsError:   res.IsError,
			})
		}
		req.Messages = append(req.Messages, results)
		reply.Reset()
	}

	text := strings.TrimSpace(reply.String())
	if text == "" {
		return fmt.Errorf("runtime: %s produced no reply", agentID)
	}
	// Stay inside the trigger's thread when already in one; top-level
	// prompts get top-level replies so channel transcripts stay linear.
	_, err := r.cfg.Bus.Post(bus.Message{
		Channel: trigger.Channel,
		Thread:  trigger.Thread,
		Author:  agentID,
		Text:    text,
	})
	return err
}

// prompt flattens recent history into a Request grounded with the
// workspace layout so models emit valid vpaths.
func (r *Runtime) prompt(e *entry, trigger bus.Message) provider.Request {
	system := strings.TrimSpace(e.m.System)
	var members []string
	for _, m := range r.cfg.WS.Members() {
		members = append(members, m.Name)
	}
	grounding := "\n\nFiles are addressed as <member>/<rel-path>. Members: " + strings.Join(members, ", ")
	system += grounding
	if r.cfg.Standards {
		system += "\n\n" + standards.Resolve(r.cfg.WS.Root, e.m.ID, r.teamLookup())
	}
	req := provider.Request{
		Model:     e.m.Model,
		System:    system,
		MaxTokens: 4096,
	}
	for _, d := range e.reg.Defs(e.m.Tools) {
		schema := json.RawMessage(d.InputSchema)
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		req.Tools = append(req.Tools, provider.ToolDef{Name: d.Name, Description: d.Description, InputSchema: schema})
	}
	history := r.cfg.Bus.History(trigger.Channel, 0)
	if n := len(history); n > historyWindow {
		history = history[n-historyWindow:]
	}
	for _, m := range history {
		role := provider.User
		if m.Author == e.m.ID {
			role = provider.Assistant
		}
		text := m.Text
		if role == provider.User && m.ID == trigger.ID {
			text = stripMention(text, e.m.ID)
		}
		req.Messages = append(req.Messages, provider.Message{
			Role:   role,
			Blocks: []provider.Block{provider.Text{Value: text}},
		})
	}
	return req
}

// teamLookup adapts the org registry for standards resolution; nil org
// yields a lookup that matches nothing.
func (r *Runtime) teamLookup() standards.TeamLookup {
	if r.cfg.Org == nil {
		return nil
	}
	return func(agentID string) []string { return r.cfg.Org.TeamsOf(agentID) }
}

// stripMention removes this agent's own @token from the trigger text.
func stripMention(text, id string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "@"+id, ""))
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
