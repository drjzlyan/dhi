package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// maxReadBytes caps read output fed to a model.
const maxReadBytes = 256 << 10

// Deps wires the builtins to workspace, sandbox, and search seams.
// One Deps is assembled per agent (Guard carries that agent's manifest
// policy; AgentID labels its approval requests).
type Deps struct {
	WS        *workspace.Workspace
	Guard     *sandbox.Guard
	Approvals *Approvals
	Searcher  search.Searcher // optional; nil disables the search tool
	AgentID   string
}

// Builtins returns the native tools. All of them resolve vpaths through
// the workspace, then consult the Guard before touching disk.
func Builtins(d Deps) []Tool {
	out := []Tool{
		readTool{d},
		writeTool{d},
		listTool{d},
	}
	if d.Searcher != nil {
		out = append(out, searchTool{d})
	}
	return out
}

// resolve parses a vpath argument and maps it to an absolute path.
func (d Deps) resolve(vpath string) (workspace.VPath, string, error) {
	vp, err := workspace.ParseVPath(vpath)
	if err != nil {
		return vp, "", fmt.Errorf("bad path %q: %w", vpath, err)
	}
	abs, err := d.WS.Resolve(vp)
	if err != nil {
		return vp, "", err
	}
	return vp, abs, nil
}

// gate checks op against abs; Allow passes, Ask parks an approval and
// blocks until a human decides, Deny errors immediately.
func gate(ctx context.Context, d Deps, op sandbox.Op, abs, display string) error {
	dec := d.Guard.Check(op, abs)
	switch dec.Effect {
	case sandbox.Allow:
		return nil
	case sandbox.Ask:
		if d.Approvals == nil {
			return fmt.Errorf("denied: %s (no approval channel configured)", dec.Reason)
		}
		return d.Approvals.wait(ctx, d.AgentID, op, display, dec.Reason)
	default:
		return fmt.Errorf("denied: %s", dec.Reason)
	}
}

type readTool struct{ Deps }

func (readTool) Def() Def {
	return Def{
		Name:        "read",
		Description: "Read a workspace file addressed as <member>/<rel-path>.",
		InputSchema: `{"type":"object","properties":{"path":{"type":"string","description":"vpath like api/main.go"},"max_bytes":{"type":"integer","description":"cap on returned bytes"}},"required":["path"]}`,
	}
}

func (t readTool) Exec(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path     string `json:"path"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("read: bad input: %w", err)
	}
	vp, abs, err := t.resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if err := gate(ctx, t.Deps, sandbox.OpRead, abs, vp.String()); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", vp.String(), err)
	}
	limit := in.MaxBytes
	if limit <= 0 || limit > maxReadBytes {
		limit = maxReadBytes
	}
	if len(data) > limit {
		return string(data[:limit]) + fmt.Sprintf("\n… [truncated %d of %d bytes]", len(data)-limit, len(data)), nil
	}
	return string(data), nil
}

type writeTool struct{ Deps }

func (writeTool) Def() Def {
	return Def{
		Name:        "write",
		Description: "Create or overwrite a workspace file (<member>/<rel-path>) with content.",
		InputSchema: `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`,
	}
}

func (t writeTool) Exec(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("write: bad input: %w", err)
	}
	vp, abs, err := t.resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := gate(ctx, t.Deps, sandbox.OpWrite, abs, vp.String()); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("write %s: %w", vp.String(), err)
	}
	if err := os.WriteFile(abs, []byte(in.Content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", vp.String(), err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), vp.String()), nil
}

type listTool struct{ Deps }

func (listTool) Def() Def {
	return Def{
		Name:        "list",
		Description: "List a directory inside a member repo (vpath; empty rel lists the member root).",
		InputSchema: `{"type":"object","properties":{"path":{"type":"string","description":"vpath like api/ or api"}},"required":["path"]}`,
	}
}

func (t listTool) Exec(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("list: bad input: %w", err)
	}
	vp, abs, err := t.resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("list: %w", err)
	}
	if err := gate(ctx, t.Deps, sandbox.OpRead, abs, vp.String()); err != nil {
		return "", fmt.Errorf("list: %w", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", vp.String(), err)
	}
	var sb strings.Builder
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		sb.WriteString(name + "\n")
	}
	return strings.TrimSuffix(sb.String(), "\n"), nil
}

type searchTool struct{ Deps }

func (searchTool) Def() Def {
	return Def{
		Name:        "search",
		Description: "Fixed-string search across member repos with ripgrep; returns vpath:line matches.",
		InputSchema: `{"type":"object","properties":{"query":{"type":"string"},"member":{"type":"string","description":"restrict to one member repo"}},"required":["query"]}`,
	}
}

const maxHits = 100

func (t searchTool) Exec(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Query  string `json:"query"`
		Member string `json:"member"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("search: bad input: %w", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return "", fmt.Errorf("search: empty query")
	}
	roots := make([]string, 0, len(t.WS.Members()))
	for _, m := range t.WS.Members() {
		if in.Member != "" && m.Name != in.Member {
			continue
		}
		roots = append(roots, m.Path)
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("search: unknown member %q", in.Member)
	}
	ch, err := t.Searcher.Search(ctx, in.Query, roots)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}
	var sb strings.Builder
	n := 0
	for hit := range ch {
		if n >= maxHits {
			fmt.Fprintf(&sb, "… [capped at %d hits]\n", maxHits)
			break
		}
		loc := hit.Path
		if vp, err := t.WS.VPathFor(hit.Path); err == nil {
			loc = vp.String()
		}
		fmt.Fprintf(&sb, "%s:%d:%s\n", loc, hit.Line, hit.Text)
		n++
	}
	if n == 0 {
		return "no matches", nil
	}
	return strings.TrimSuffix(sb.String(), "\n"), nil
}
