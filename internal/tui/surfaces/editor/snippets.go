package editor

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/vscsnippets"
)

// Snippets (F-171). A completion item in snippet syntax (LSP
// insertTextFormat 2, or a user snippet from <repo>/.vscode/*.code-snippets)
// is expanded into the buffer and starts a tab-stop session.
//
// Insert-mode tab precedence, first match wins:
//  1. the completion popup is open: tab accepts the selected item;
//  2. a snippet session is live: tab / shift+tab jump between stops;
//  3. otherwise tab indents.

type userSnipSet struct {
	sig   string
	snips []vscsnippets.Snippet
	errs  []error
}

// userSnippets returns the .vscode snippets of the member that holds path
// (cached; re-read when a snippet file changes) and reports a parse
// failure once per change on the status line, never silently (ADR-0011).
func (m *Model) userSnippets(e *textbuf.Editor) []vscsnippets.Snippet {
	root := m.memberRootOf(e.Path())
	if root == "" {
		return nil
	}
	if m.userSnips == nil {
		m.userSnips = map[string]userSnipSet{}
	}
	got, sig, errs := vscsnippets.Dir(root)
	old, seen := m.userSnips[root]
	if !seen || old.sig != sig {
		m.userSnips[root] = userSnipSet{sig: sig, snips: got, errs: errs}
		if len(errs) > 0 {
			e.SetMessage("snippets: " + errs[0].Error())
		}
	}
	return m.userSnips[root].snips
}

func (m *Model) hasUserSnippets() bool {
	e := m.active()
	return e != nil && len(m.userSnippets(e)) > 0
}

// userSnippetItems offers the snippets whose prefix starts with the word
// before the cursor, as snippet-format completion items.
func (m *Model) userSnippetItems(e *textbuf.Editor) []lsp.CompletionItem {
	all := m.userSnippets(e)
	if len(all) == 0 {
		return nil
	}
	cur := e.Buffer().Cursor()
	r := []rune(e.Buffer().Line(cur.Line))
	start := min(cur.Col, len(r))
	for start > 0 && isIdent(r[start-1]) {
		start--
	}
	word := strings.ToLower(string(r[start:min(cur.Col, len(r))]))
	_, lang, _ := m.langs.For(e.Path())
	var out []lsp.CompletionItem
	for _, s := range all {
		if !s.Matches(lang) || !strings.HasPrefix(strings.ToLower(s.Prefix), word) {
			continue
		}
		detail := "snippet"
		if s.Desc != "" {
			detail += ": " + s.Desc
		}
		out = append(out, lsp.CompletionItem{Label: s.Prefix, Detail: detail, Insert: s.Body, Snippet: true})
	}
	return out
}

// expandSnippet replaces from..to with the expanded snippet and opens a
// tab-stop session when it has numbered stops.
func (m *Model) expandSnippet(e *textbuf.Editor, from, to textbuf.Pos, src string) {
	line := []rune(e.Buffer().Line(from.Line))
	indent := 0
	for indent < len(line) && (line[indent] == ' ' || line[indent] == '\t') {
		indent++
	}
	sn := textbuf.ExpandSnippet(src, m.snippetVars(e, from), string(line[:min(indent, from.Col)]))
	m.snip = e.InsertSnippet(from, to, sn)
	m.snipNote(e)
}

func (m *Model) snipNote(e *textbuf.Editor) {
	if m.snip != nil {
		e.SetMessage("snippet " + m.snip.Progress() + "  tab next · shift+tab back · esc done")
	}
}

// snippetVars resolves the common VS Code variables.
func (m *Model) snippetVars(e *textbuf.Editor, at textbuf.Pos) func(string) (string, bool) {
	path := e.Path()
	now := time.Now()
	line := e.Buffer().Line(at.Line)
	return func(name string) (string, bool) {
		switch name {
		case "TM_FILENAME":
			return filepath.Base(path), true
		case "TM_FILENAME_BASE":
			b := filepath.Base(path)
			return strings.TrimSuffix(b, filepath.Ext(b)), true
		case "TM_DIRECTORY":
			return filepath.Dir(path), true
		case "TM_FILEPATH":
			return path, true
		case "RELATIVE_FILEPATH":
			return m.memberRel(path), true
		case "WORKSPACE_FOLDER":
			return m.memberRootOf(path), true
		case "WORKSPACE_NAME":
			return filepath.Base(m.memberRootOf(path)), true
		case "TM_LINE_INDEX":
			return strconv.Itoa(at.Line), true
		case "TM_LINE_NUMBER":
			return strconv.Itoa(at.Line + 1), true
		case "TM_CURRENT_LINE":
			return line, true
		case "TM_CURRENT_WORD":
			w, _ := wordAt(line, at.Col)
			return w, true
		case "TM_SELECTED_TEXT":
			return "", true
		case "CURRENT_YEAR":
			return now.Format("2006"), true
		case "CURRENT_YEAR_SHORT":
			return now.Format("06"), true
		case "CURRENT_MONTH":
			return now.Format("01"), true
		case "CURRENT_DATE":
			return now.Format("02"), true
		case "CURRENT_HOUR":
			return now.Format("15"), true
		case "CURRENT_MINUTE":
			return now.Format("04"), true
		case "CURRENT_SECOND":
			return now.Format("05"), true
		}
		return "", false
	}
}

// snipKey runs before the buffer sees an insert-mode key. It reports true
// when the session consumed it (a tab jump, or backspace over a selected
// placeholder).
func (m *Model) snipKey(e *textbuf.Editor, key string) bool {
	if m.snip == nil {
		return false
	}
	if m.snip.Editor() != e || e.Mode() != textbuf.ModeInsert {
		m.snip = nil
		return false
	}
	consumed, done := m.snip.BeforeKey(key)
	if done {
		m.snip = nil
		e.SetMessage("")
	} else if consumed {
		m.snipNote(e)
	}
	if consumed {
		m.lspSync()
	}
	return consumed
}

// snipAfterKey folds the key's edit into the session after the buffer
// handled it: mirrors update, and leaving the stop ends the session.
func (m *Model) snipAfterKey(e *textbuf.Editor) {
	if m.snip == nil {
		return
	}
	if m.snip.Editor() != e || e.Mode() != textbuf.ModeInsert || !m.snip.Sync() {
		m.snip = nil
		e.SetMessage("")
	}
}
