package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// ideationTools is the ideation surface (F-030 P1, extended by F-033):
// list/read sessions and artifacts, and the round-table writes —
// artifact_create/artifact_edit (mutating, approval-gated via the Write
// scope) and propose_session (an agent may only *propose* a session or
// breakout; the human accepts it). The session channel remains readable
// through channel_read.
func (d Deps) ideationTools() []tool {
	if d.Sessions == nil {
		return nil
	}
	return []tool{
		{
			info: mcp.ToolInfo{
				Name:        "ideation_list",
				Description: "List ideation sessions. Args: {}. Returns id, name, topic and artifact count.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
			parse: func(json.RawMessage) (any, error) { return nil, nil },
			exec: func(_ context.Context, _ any) (string, error) {
				sessions := d.Sessions.Sessions()
				if len(sessions) == 0 {
					return "(no sessions)\n", nil
				}
				var b strings.Builder
				for _, s := range sessions {
					parent := ""
					if s.IsBreakout() {
						parent = "  ← " + s.Parent
					}
					fmt.Fprintf(&b, "%s  %s — %s [%s, %d artifact(s)]%s\n",
						s.ID, s.Name, s.Topic, s.Mode, len(s.Artifacts), parent)
				}
				return b.String(), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "ideation_read",
				Description: "Read an ideation artifact. Args: {\"session\": \"<id>\", \"path\": \"plan.md\"}.",
				InputSchema: json.RawMessage(`{"type":"object","required":["session","path"],"properties":{"session":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: d.parseArtifact(),
			exec: func(_ context.Context, dec any) (string, error) {
				p := dec.(artifactPlan)
				info, err := os.Stat(p.abs)
				if err != nil {
					return "", fmt.Errorf("%s: %w", p.rel, err)
				}
				if info.Size() > maxReadBytes {
					return "", fmt.Errorf("%s is %d bytes (over the %d-byte read cap)", p.rel, info.Size(), maxReadBytes)
				}
				data, err := os.ReadFile(p.abs)
				if err != nil {
					return "", fmt.Errorf("%s: %w", p.rel, err)
				}
				return string(data), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "session_read",
				Description: "Read an ideation session's round-table state. Args: {\"session\": \"<id>\"}. Returns mode, moderator, participants, floor holder, turns and artifacts.",
				InputSchema: json.RawMessage(`{"type":"object","required":["session"],"properties":{"session":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Session string `json:"session"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.Session) == "" {
					return nil, fmt.Errorf("session is required")
				}
				sess, ok := d.Sessions.Get(strings.TrimSpace(a.Session))
				if !ok {
					return nil, fmt.Errorf("unknown session %q", a.Session)
				}
				return sess, nil
			},
			exec: func(_ context.Context, dec any) (string, error) {
				return renderSession(dec.(ideation.Session)), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "artifact_create",
				Description: "Create a new ideation artifact (markdown or mermaid). Args: {\"session\": \"<id>\", \"path\": \"design.md\", \"content\": \"...\"}. Refuses if the file already exists — use artifact_edit.",
				InputSchema: artifactSchema("Create"),
			},
			parse: d.parseArtifactWrite(true),
			exec: func(_ context.Context, dec any) (string, error) {
				return d.writeArtifact(dec.(artifactWrite))
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "artifact_edit",
				Description: "Replace an existing ideation artifact's content. Args: {\"session\": \"<id>\", \"path\": \"design.md\", \"content\": \"...\"}. Refuses if the file does not exist — use artifact_create.",
				InputSchema: artifactSchema("Edit"),
			},
			parse: d.parseArtifactWrite(false),
			exec: func(_ context.Context, dec any) (string, error) {
				return d.writeArtifact(dec.(artifactWrite))
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "propose_session",
				Description: "Propose a new ideation session or breakout for the human to accept. Args: {\"name\": \"...\", \"topic\": \"...\", \"mode\": \"1:1|group|breakout\", \"parent\": \"<session-id>\", \"participants\": [\"scout\"]}. Agents cannot open a session directly.",
				InputSchema: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"topic":{"type":"string"},"mode":{"type":"string"},"parent":{"type":"string"},"participants":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Name         string   `json:"name"`
					Topic        string   `json:"topic"`
					Mode         string   `json:"mode"`
					Parent       string   `json:"parent"`
					Participants []string `json:"participants"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.Name) == "" {
					return nil, fmt.Errorf("name is required")
				}
				return a, nil
			},
			exec: func(_ context.Context, dec any) (string, error) {
				a := dec.(struct {
					Name         string   `json:"name"`
					Topic        string   `json:"topic"`
					Mode         string   `json:"mode"`
					Parent       string   `json:"parent"`
					Participants []string `json:"participants"`
				})
				caller := ""
				if d.Agent != nil {
					caller = d.Agent.ID
				}
				p, err := d.Sessions.Propose(caller, a.Name, a.Topic, ideation.SessionMode(a.Mode), a.Parent, a.Participants)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("proposed session %q (%s) as #%d — awaiting the human's accept/decline\n", p.Name, p.Mode, p.ID), nil
			},
		},
	}
}

// artifactSchema is the shared create/edit input schema (verbated).
func artifactSchema(verb string) json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["session","path","content"],"properties":{"session":{"type":"string"},"path":{"type":"string"},"content":{"type":"string","description":"` + verb + ` the artifact body"}},"additionalProperties":false}`)
}

// parseArtifact validates an existing artifact reference (ideation_read).
func (d Deps) parseArtifact() func(json.RawMessage) (any, error) {
	return func(raw json.RawMessage) (any, error) {
		var a struct {
			Session string `json:"session"`
			Path    string `json:"path"`
		}
		if err := args(raw, &a); err != nil {
			return nil, err
		}
		if strings.TrimSpace(a.Session) == "" || strings.TrimSpace(a.Path) == "" {
			return nil, fmt.Errorf("session and path are required")
		}
		if _, ok := d.Sessions.Get(strings.TrimSpace(a.Session)); !ok {
			return nil, fmt.Errorf("unknown session %q", a.Session)
		}
		rel := strings.TrimSpace(a.Path)
		if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			return nil, fmt.Errorf("path %q must be relative to the session folder", rel)
		}
		abs := d.Sessions.ArtifactPath(a.Session, rel)
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("unknown artifact %q in %s", rel, a.Session)
		}
		return artifactPlan{session: a.Session, rel: rel, abs: abs}, nil
	}
}

// parseArtifactWrite validates a create/edit plan, enforcing the
// create-vs-edit existence rule before the approval prompt.
func (d Deps) parseArtifactWrite(create bool) func(json.RawMessage) (any, error) {
	return func(raw json.RawMessage) (any, error) {
		var a struct {
			Session string `json:"session"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := args(raw, &a); err != nil {
			return nil, err
		}
		session := strings.TrimSpace(a.Session)
		if session == "" || strings.TrimSpace(a.Path) == "" {
			return nil, fmt.Errorf("session and path are required")
		}
		if _, ok := d.Sessions.Get(session); !ok {
			return nil, fmt.Errorf("unknown session %q", session)
		}
		rel := strings.TrimSpace(a.Path)
		if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			return nil, fmt.Errorf("path %q must be relative to the session folder", rel)
		}
		abs := d.Sessions.ArtifactPath(session, rel)
		_, err := os.Stat(abs)
		exists := err == nil
		if create && exists {
			return nil, fmt.Errorf("artifact %q already exists in %s — use artifact_edit", rel, session)
		}
		if !create && !exists {
			return nil, fmt.Errorf("artifact %q not found in %s — use artifact_create", rel, session)
		}
		return artifactWrite{session: session, rel: rel, abs: abs, content: a.Content, create: create}, nil
	}
}

// writeArtifact persists content, rescans the session (content-hash
// status reset is Scan's job) and claims authorship when unclaimed.
func (d Deps) writeArtifact(w artifactWrite) (string, error) {
	if err := os.MkdirAll(filepath.Dir(w.abs), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(w.abs, []byte(w.content), 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := d.Sessions.Scan(w.session); err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}
	if d.Agent != nil && d.Agent.ID != "" {
		if a, ok := d.Sessions.Artifact(w.session, w.rel); ok && a.Author == "" {
			_ = d.Sessions.ClaimAuthor(w.session, w.rel, d.Agent.ID)
		}
	}
	verb := "updated"
	if w.create {
		verb = "created"
	}
	return fmt.Sprintf("%s %s in %s\n", verb, w.rel, w.session), nil
}

// renderSession formats the round-table state for an agent.
func renderSession(s ideation.Session) string {
	var b strings.Builder
	fmt.Fprintf(&b, "session %s  %q — %s\n", s.ID, s.Name, s.Topic)
	mod := s.Moderator
	if mod == "" {
		mod = "you"
	}
	fmt.Fprintf(&b, "mode: %s · moderator: %s · channel: %s\n", s.Mode, mod, s.Channel)
	if s.IsBreakout() {
		fmt.Fprintf(&b, "parent: %s\n", s.Parent)
	}
	fmt.Fprintf(&b, "participants: %s\n", strings.Join(s.Agents, ", "))
	if holder := s.CurrentSpeaker(); holder == "" {
		fmt.Fprintf(&b, "floor: the moderator (%d turn(s))\n", len(s.Turns))
	} else {
		fmt.Fprintf(&b, "floor: %s (%d turn(s))\n", holder, len(s.Turns))
	}
	if len(s.Artifacts) == 0 {
		b.WriteString("artifacts: (none)\n")
		return b.String()
	}
	b.WriteString("artifacts:\n")
	for _, a := range s.Artifacts {
		author := a.Author
		if author == "" {
			author = "unclaimed"
		}
		fmt.Fprintf(&b, "  %s  %s  by %s\n", a.Path, a.Status, author)
	}
	return b.String()
}

type artifactPlan struct {
	session, rel, abs string
}

type artifactWrite struct {
	session, rel, abs string
	content           string
	create            bool
}
