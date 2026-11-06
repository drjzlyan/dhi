package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/lsp"
)

// symbolCap bounds one agent symbols result (F-145).
const symbolCap = 200

// symbolsCall serves the agent's code_symbols modes (F-145) from the
// language servers the editor already runs. Nothing is started or
// installed here: no running server for the language is a plain
// "no language server" answer (ADR-0011).
//
//	symbols_document  abs = file                  → its outline
//	symbols_workspace abs = file (optional) picks the language,
//	                  arg = query                 → matching symbols
func (m *Model) symbolsCall(op, abs, arg string) (string, error) {
	if op == "symbols_document" {
		c := m.clientFor(abs)
		if c == nil {
			return m.noServer(abs), nil
		}
		syms, err := withTimeout(func() ([]lsp.Symbol, error) { return c.DocumentSymbols(abs) })
		if err != nil {
			return "", err
		}
		if len(syms) == 0 {
			return "(no symbols)", nil
		}
		var b strings.Builder
		for i, s := range syms {
			if i == symbolCap {
				fmt.Fprintf(&b, "(truncated: showing %d of %d symbols)\n", symbolCap, len(syms))
				break
			}
			fmt.Fprintf(&b, "%s%s %s :%d\n", strings.Repeat("  ", s.Depth), s.Kind, s.Name, s.Line+1)
		}
		return b.String(), nil
	}

	var clients []*lsp.Client
	if abs != "" {
		c := m.clientFor(abs)
		if c == nil {
			return m.noServer(abs), nil
		}
		clients = []*lsp.Client{c}
	} else if m.lspMgr != nil {
		for _, lc := range m.lspMgr.Live() {
			clients = append(clients, lc.Client)
		}
	}
	if len(clients) == 0 {
		return "no language server is running (open a source file in the editor to start one)", nil
	}
	var all []lsp.WorkspaceSymbol
	for _, c := range clients {
		syms, err := withTimeout(func() ([]lsp.WorkspaceSymbol, error) { return c.WorkspaceSymbols(arg) })
		if err != nil {
			return "", err
		}
		all = append(all, syms...)
	}
	// Only symbols inside a workspace member are reported, as member/rel
	// paths: servers also index the standard library and dependencies.
	var b strings.Builder
	shown, inside := 0, 0
	for _, s := range all {
		vp, ok := m.memberPath(s.Path)
		if !ok {
			continue
		}
		inside++
		if shown == symbolCap {
			continue
		}
		name := s.Name
		if s.Container != "" {
			name = s.Container + "." + name
		}
		fmt.Fprintf(&b, "%s %s  %s:%d\n", s.Kind, name, vp, s.Line+1)
		shown++
	}
	if inside == 0 {
		return "(no matching symbols)", nil
	}
	if inside > shown {
		fmt.Fprintf(&b, "(truncated: showing %d of %d symbols; narrow the query)\n", shown, inside)
	}
	return b.String(), nil
}

// noServer names the missing server for a file's language.
func (m *Model) noServer(abs string) string {
	if l, _, ok := m.langs.For(abs); ok {
		return "no language server for " + l.Name + " (none is running; open a " + l.Name + " file in the editor to start " + l.Server + ")"
	}
	ext := filepath.Ext(abs)
	if ext == "" {
		ext = filepath.Base(abs)
	}
	return "no language server for " + ext + " (DHI has no server for it)"
}

// memberPath renders an absolute path as member/rel when it lies inside
// a workspace member.
func (m *Model) memberPath(abs string) (string, bool) {
	for _, mem := range m.members {
		rel, err := filepath.Rel(mem.path, abs)
		if err == nil && rel != ".." && !hasDotDotPrefix(rel) && !filepath.IsAbs(rel) {
			return mem.name + "/" + filepath.ToSlash(rel), true
		}
	}
	return "", false
}
