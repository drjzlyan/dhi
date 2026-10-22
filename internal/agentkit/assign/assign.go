// Package assign decides which agents get a skill or an MCP server
// (F-107). Recommend scores every agent against what the skill or server
// is for — deterministic, explained, no model call — so an install ends
// with a sensible preselection the user can accept, widen to every bot,
// or change. Apply writes the choice into the agents' manifests, all or
// nothing; the "all bots" policy instead lives in installed.toml and is
// merged at run time (Effective), so agents created later get it too.
package assign

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/mcpbridge"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Kinds of things assigned.
const (
	KindSkill  = "skill"
	KindServer = "server"
)

// Item is a skill or server to place.
type Item struct {
	Kind        string
	Name        string
	Description string
	Category    string   // a plugin's category, when known
	Tools       []string // a server's tool names, when known
}

// Pick is one recommended agent and why.
type Pick struct {
	AgentID string
	Score   int
	Reason  string
}

// topic is a kind of work and the roles that do it.
type topic struct {
	name  string
	words []string
	roles []string
}

// topics map what a skill or server is about to the built-in roles that
// do that work. Words are matched whole, lowercased.
var topics = []topic{
	{"issue tracking", []string{"jira", "linear", "issue", "issues", "ticket", "tickets", "sprint", "backlog", "asana", "epic", "triage", "bug", "bugs"},
		[]string{"lead", "planner", "product-manager", "tester", "fixer"}},
	{"team chat", []string{"slack", "teams", "discord", "message", "messages", "channel", "channels", "chat", "thread"},
		[]string{"lead", "product-manager", "scribe"}},
	{"meetings", []string{"zoom", "meeting", "meetings", "calendar", "transcript", "transcripts", "recording", "whiteboard", "webinar"},
		[]string{"lead", "product-manager", "scribe", "planner"}},
	{"docs and wikis", []string{"confluence", "notion", "wiki", "docs", "documentation", "page", "pages", "knowledge", "readme", "writing"},
		[]string{"scribe", "product-manager", "planner", "lead"}},
	{"code review", []string{"review", "reviews", "reviewing", "diff", "pull", "pr", "prs", "patch", "lint"},
		[]string{"reviewer", "fixer"}},
	{"source control", []string{"github", "gitlab", "bitbucket", "git", "repository", "repositories", "commit", "branch"},
		[]string{"backend", "frontend", "fullstack", "devops", "reviewer", "fixer"}},
	{"build and deploy", []string{"deploy", "deployment", "ci", "cd", "pipeline", "pipelines", "terraform", "kubernetes", "docker", "infra", "infrastructure", "release", "cloud", "aws", "gcp", "azure", "incident", "incidents", "outage", "monitoring"},
		[]string{"devops", "fixer"}},
	{"testing", []string{"test", "tests", "testing", "qa", "coverage", "playwright", "e2e", "regression"},
		[]string{"tester", "fixer", "fullstack"}},
	{"design", []string{"figma", "design", "designs", "ui", "ux", "css", "accessibility", "wireframe", "wireframes", "component", "components"},
		[]string{"designer", "frontend"}},
	{"data", []string{"sql", "database", "databases", "postgres", "mysql", "analytics", "data", "warehouse", "bigquery", "query"},
		[]string{"backend", "fullstack"}},
	{"api", []string{"api", "apis", "rest", "graphql", "endpoint", "endpoints", "sdk", "webhook", "webhooks"},
		[]string{"backend", "fullstack"}},
	{"product", []string{"spec", "specs", "requirements", "roadmap", "prd", "customer", "customers", "feedback", "priorities"},
		[]string{"product-manager", "planner", "lead"}},
}

var stop = map[string]bool{"the": true, "a": true, "an": true, "and": true, "or": true, "to": true, "of": true, "for": true, "in": true,
	"on": true, "with": true, "use": true, "when": true, "this": true, "that": true, "it": true, "is": true, "be": true, "your": true,
	"you": true, "from": true, "by": true, "as": true, "at": true, "any": true, "into": true, "mcp": true, "server": true, "skill": true,
	"tools": true, "tool": true, "agent": true, "agents": true, "work": true, "workflow": true, "help": true, "helps": true}

func words(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var out []string
	for _, w := range f {
		if len(w) > 1 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

// Recommend scores every agent for item, best first; only agents with a
// reason are returned. When nobody matches, the team leads are offered
// (they can delegate), so there is always a starting point.
func Recommend(item Item, roster []*manifest.Agent, lib *library.Store, o *org.Org) []Pick {
	itemWords := words(item.Name + " " + item.Description + " " + item.Category + " " + strings.Join(item.Tools, " "))
	itemSet := map[string]bool{}
	for _, w := range itemWords {
		itemSet[w] = true
	}
	var hit []topic
	for _, t := range topics {
		for _, w := range t.words {
			if itemSet[w] {
				hit = append(hit, t)
				break
			}
		}
	}
	var out []Pick
	for _, a := range roster {
		score, reasons := 0, []string{}
		for _, t := range hit {
			for _, r := range t.roles {
				if a.Role == r {
					score += 3
					reasons = append(reasons, a.Role+" works with "+t.name)
					break
				}
			}
		}
		profile := map[string]bool{}
		for _, w := range words(agentText(a, lib)) {
			profile[w] = true
		}
		var shared []string
		for w := range itemSet {
			if profile[w] {
				shared = append(shared, w)
			}
		}
		sort.Strings(shared)
		if n := len(shared); n > 0 {
			score += min(n, 3)
			reasons = append(reasons, "mentions "+strings.Join(shared[:min(n, 3)], ", "))
		}
		if hasItem(a, item) {
			continue // already has it
		}
		// A role that does this work qualifies; a single shared word
		// ("review" in a designer's "UX review") does not.
		if score >= 2 {
			out = append(out, Pick{AgentID: a.ID, Score: score, Reason: strings.Join(reasons, "; ")})
		}
	}
	if len(out) == 0 && o != nil {
		for _, t := range o.Teams() {
			if t.Lead == "" || t.Lead == org.Human {
				continue
			}
			for _, a := range roster {
				if a.ID == t.Lead && !hasItem(a, item) {
					out = append(out, Pick{AgentID: a.ID, Score: 1, Reason: "no clear match; leads " + t.Name + " and can delegate"})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].AgentID < out[j].AgentID
	})
	return out
}

// agentText is what an agent is for: role, persona, system prompt, its
// skills' descriptions and its tools.
func agentText(a *manifest.Agent, lib *library.Store) string {
	parts := []string{a.ID, a.Name, a.Role, a.System, strings.Join(a.Tools, " ")}
	if lib != nil {
		if r, ok := lib.Role(a.Role); ok {
			parts = append(parts, r.Description)
		}
		if p, ok := lib.Persona(a.Persona); ok {
			parts = append(parts, p.Description)
		}
		for _, s := range a.Skills {
			if k, ok := lib.Skill(s); ok {
				parts = append(parts, k.Description)
			}
		}
	}
	return strings.Join(parts, " ")
}

// ServerTool is the allowlist entry that gives an agent every tool of a
// server.
func ServerTool(server string) string { return mcpbridge.ToolPrefix + server + "__*" }

func hasItem(a *manifest.Agent, it Item) bool {
	if it.Kind == KindServer {
		return contains(a.Tools, ServerTool(it.Name))
	}
	return contains(a.Skills, it.Name)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Apply gives item to exactly the agents in ids and takes it from every
// other agent that had it (the dialog shows the full set). Every manifest
// is checked before any is written.
func Apply(ws *workspace.Workspace, o *org.Org, it Item, ids []string) (added, removed []string, err error) {
	roster, err := org.LoadRoster(ws)
	if err != nil {
		return nil, nil, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	known := map[string]bool{}
	type change struct {
		a   *manifest.Agent
		add bool
	}
	var todo []change
	for _, a := range roster {
		known[a.ID] = true
		has := hasItem(a, it)
		switch {
		case want[a.ID] && !has:
			todo = append(todo, change{a, true})
		case !want[a.ID] && has:
			todo = append(todo, change{a, false})
		}
	}
	for id := range want {
		if !known[id] {
			return nil, nil, fmt.Errorf("unknown agent %q", id)
		}
	}
	updated := make([]*manifest.Agent, 0, len(todo))
	for _, c := range todo {
		cp := *c.a
		cp.Skills = append([]string(nil), c.a.Skills...)
		cp.Tools = append([]string(nil), c.a.Tools...)
		switch {
		case it.Kind == KindSkill && c.add:
			cp.Skills = append(cp.Skills, it.Name)
		case it.Kind == KindSkill:
			cp.Skills = without(cp.Skills, it.Name)
		case c.add:
			cp.Tools = append(cp.Tools, ServerTool(it.Name))
		default:
			cp.Tools = without(cp.Tools, ServerTool(it.Name))
		}
		data, err := manifest.Marshal(&cp)
		if err == nil {
			_, err = manifest.Parse(cp.ID, data)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", cp.ID, err)
		}
		updated = append(updated, &cp)
	}
	for i, a := range updated {
		if err := o.UpdateAgent(ws, a); err != nil {
			return added, removed, err
		}
		if todo[i].add {
			added = append(added, a.ID)
		} else {
			removed = append(removed, a.ID)
		}
	}
	return added, removed, nil
}

func without(xs []string, x string) []string {
	out := xs[:0]
	for _, y := range xs {
		if y != x {
			out = append(out, y)
		}
	}
	return out
}

// Holders lists the agents that have item in their manifest.
func Holders(roster []*manifest.Agent, it Item) []string {
	var out []string
	for _, a := range roster {
		if hasItem(a, it) {
			out = append(out, a.ID)
		}
	}
	sort.Strings(out)
	return out
}

// Policy is the "all bots" set (from installed.toml, both scopes).
type Policy struct {
	Skills  []string
	Servers []string
}

// Effective is a with the all-bots skills and servers added: what the
// runtime runs. a itself is not changed.
func Effective(a *manifest.Agent, p Policy) *manifest.Agent {
	if a == nil || (len(p.Skills) == 0 && len(p.Servers) == 0) {
		return a
	}
	cp := *a
	cp.Skills = append([]string(nil), a.Skills...)
	cp.Tools = append([]string(nil), a.Tools...)
	for _, s := range p.Skills {
		if !contains(cp.Skills, s) {
			cp.Skills = append(cp.Skills, s)
		}
	}
	for _, s := range p.Servers {
		if t := ServerTool(s); !contains(cp.Tools, t) {
			cp.Tools = append(cp.Tools, t)
		}
	}
	return &cp
}

// PolicyFrom merges the all-bots sets of the given locks.
func PolicyFrom(locks ...*install.Lock) Policy {
	var p Policy
	seen := map[string]bool{}
	for _, l := range locks {
		if l == nil {
			continue
		}
		for _, s := range l.AllBots.Skills {
			if !seen["s/"+s] {
				seen["s/"+s] = true
				p.Skills = append(p.Skills, s)
			}
		}
		for _, s := range l.AllBots.Servers {
			if !seen["v/"+s] {
				seen["v/"+s] = true
				p.Servers = append(p.Servers, s)
			}
		}
	}
	return p
}
