package editor

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// File operations in the tree (F-119): a new file or folder, rename or
// move, delete to DHI's trash. One small dialog drives each; the tree and
// open buffers follow.

type treeOpKind uint8

const (
	opNew treeOpKind = iota + 1
	opRename
	opDelete
	opCommand   // ":" with the tree focused: ex commands that need no buffer
	opAgentEdit // ctrl+k: tell an agent how to change the selection (F-120)
)

type treeOp struct {
	kind   treeOpKind
	dir    string // opNew: the folder the new path is relative to
	node   *node  // opRename/opDelete: the target
	input  kit.Input
	err    string
	region region // opAgentEdit: what to change
}

// startTreeOp opens the dialog for kind on the tree's cursor row.
func (m *Model) startTreeOp(kind treeOpKind) bool {
	if kind == opCommand {
		m.treeOp = &treeOp{kind: opCommand, input: kit.NewInput("", "e path · 42 · diag · lsp · test all")}
		return true
	}
	n := m.cursorNode()
	if n == nil {
		return false
	}
	op := &treeOp{kind: kind, node: n}
	switch kind {
	case opNew:
		op.dir = n.path
		if n.kind == nodeFile {
			op.dir = filepath.Dir(n.path)
		}
		op.input = kit.NewInput("", "name, or path/to/name — end with / for a folder")
	case opRename:
		if n.kind == nodeRepo {
			m.treeNote = "a workspace member is renamed in Workspace → REPOS"
			return true
		}
		op.input = kit.NewInput(m.memberRel(n.path), "")
	case opDelete:
		if n.kind == nodeRepo {
			m.treeNote = "remove a member in Workspace → REPOS"
			return true
		}
	}
	m.treeOp = op
	return true
}

// memberRel is path relative to its member's root (slash-separated).
func (m *Model) memberRel(path string) string {
	for _, mem := range m.members {
		if r, err := filepath.Rel(mem.path, path); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return filepath.Base(path)
}

// memberRootOf is the member folder that holds path ("" when none).
func (m *Model) memberRootOf(path string) string {
	for _, mem := range m.members {
		if r, err := filepath.Rel(mem.path, path); err == nil && !strings.HasPrefix(r, "..") {
			return mem.path
		}
	}
	return ""
}

// treeOpKey drives the open dialog; it owns every key.
func (m *Model) treeOpKey(key string) bool {
	op := m.treeOp
	switch key {
	case "esc":
		m.treeOp = nil
		return true
	case "enter":
		if err := m.runTreeOp(op); err != nil {
			op.err = err.Error()
			return true
		}
		m.treeOp = nil
		return true
	}
	if op.kind == opDelete {
		if key == "y" {
			return m.treeOpKey("enter")
		}
		if key == "n" {
			m.treeOp = nil
		}
		return true
	}
	op.err = ""
	op.input.HandleKey(key)
	return true
}

func (m *Model) runTreeOp(op *treeOp) error {
	switch op.kind {
	case opNew:
		return m.createPath(op)
	case opRename:
		return m.renamePath(op)
	case opDelete:
		return m.trashPath(op.node)
	case opAgentEdit:
		msg := m.agentChange(op.input.Value, op.region)
		m.treeOp = nil
		if e := m.active(); e != nil {
			e.SetMessage(msg)
		}
		return nil
	case opCommand:
		cmd := strings.TrimSpace(op.input.Value)
		if cmd == "" {
			return nil
		}
		scratch := textbuf.NewEditor("")
		m.treeOp = nil
		if !m.ExecEx(scratch, cmd) {
			m.treeNote = "not an editor command here: " + cmd
		} else if msg := scratch.Message(); msg != "" {
			if e := m.active(); e != nil && m.bufFocus {
				e.SetMessage(msg)
			} else {
				m.treeNote = msg
			}
		}
		return nil
	}
	return nil
}

// insideWorkspace refuses a path outside every member (a typed "../..").
func (m *Model) insideWorkspace(path string) error {
	if m.memberRootOf(path) == "" {
		return fmt.Errorf("stays inside the workspace's repos")
	}
	return nil
}

func (m *Model) createPath(op *treeOp) error {
	name := strings.TrimSpace(op.input.Value)
	if name == "" {
		return fmt.Errorf("type a name")
	}
	folder := strings.HasSuffix(name, "/")
	target := filepath.Join(op.dir, filepath.FromSlash(strings.TrimSuffix(name, "/")))
	if err := m.insideWorkspace(target); err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(target))
	}
	if folder {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_ = f.Close()
	}
	m.refreshTree()
	m.revealPath(target)
	if !folder {
		m.open(&node{kind: nodeFile, path: target, name: filepath.Base(target)})
	}
	m.treeNote = "created " + m.memberRel(target)
	return nil
}

func (m *Model) renamePath(op *treeOp) error {
	rel := strings.TrimSpace(op.input.Value)
	if rel == "" {
		return fmt.Errorf("type the new name or path")
	}
	root := m.memberRootOf(op.node.path)
	target := filepath.Join(root, filepath.FromSlash(rel))
	if target == op.node.path {
		return nil
	}
	if err := m.insideWorkspace(target); err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s already exists", rel)
	}
	for _, t := range m.bufs { // never move a file out from under unsaved edits
		if t.ed != nil && t.ed.Buffer().Dirty() && within(t.path, op.node.path) {
			return fmt.Errorf("%s has unsaved changes — save it first", filepath.Base(t.path))
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Rename(op.node.path, target); err != nil {
		return err
	}
	for _, t := range m.bufs { // open buffers follow the move
		if within(t.path, op.node.path) {
			np := filepath.Join(target, strings.TrimPrefix(t.path, op.node.path))
			t.path = np
			t.ed.SetPath(np)
			t.ed.Buffer().NoteMoved(np)
			if vp, err := m.ws.VPathFor(np); err == nil {
				t.vp = vp.String()
			}
			t.syntax = &highlighter{path: np}
		}
	}
	if t := m.activeTabRef(); t != nil {
		m.openPath, m.openVPath = t.path, t.vp
	}
	m.items = nil // the finder reindexes
	m.refreshTree()
	m.revealPath(target)
	m.treeNote = "moved to " + rel
	return nil
}

// trashPath moves path into the workspace's .dhi/tmp/trash (ignored by
// git, recoverable) and closes buffers that showed it.
func (m *Model) trashPath(n *node) error {
	for _, t := range m.bufs {
		if t.ed != nil && t.ed.Buffer().Dirty() && within(t.path, n.path) {
			return fmt.Errorf("%s has unsaved changes — save or :q! it first", filepath.Base(t.path))
		}
	}
	bin := filepath.Join(m.ws.Root, workspace.DHIDir, "tmp", "trash", time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(bin, filepath.Base(n.path))
	if err := os.Rename(n.path, dst); err != nil {
		return fmt.Errorf("could not move it to DHI's trash (%v) — delete it in the terminal", err)
	}
	var keep []*bufTab
	for _, t := range m.bufs {
		if !within(t.path, n.path) {
			keep = append(keep, t)
		} else {
			m.lspDidClose(t.vp, t.path) // F-132: notify LSP of close
		}
	}
	m.bufs = keep
	if m.activeTab >= len(m.bufs) {
		m.activeTab = max(len(m.bufs)-1, 0)
	}
	if len(m.bufs) == 0 {
		m.bufFocus = false
		m.openPath, m.openVPath = "", ""
	}
	m.items = nil
	m.refreshTree()
	rel, _ := filepath.Rel(m.ws.Root, dst)
	m.treeNote = "moved " + n.name + " to " + filepath.ToSlash(rel) + " (move it back to restore)"
	return nil
}

// within reports p == dir or p under dir.
func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}

func (m *Model) activeTabRef() *bufTab {
	if m.activeTab < len(m.bufs) {
		return m.bufs[m.activeTab]
	}
	return nil
}

// revealPath expands the tree down to path and puts the cursor on it.
func (m *Model) revealPath(path string) {
	revealTo(m.roots, path)
	m.refreshRows()
	if n := findByPath(m.roots, path); n != nil {
		if i, ok := rowIndex(m.rows, n); ok {
			m.list.Cursor = i
		}
	}
}

// refreshTree re-reads every expanded folder so files made or removed
// outside DHI (an agent, git, the terminal) show up, keeping which
// folders are open.
func (m *Model) refreshTree() {
	for _, r := range m.roots {
		refreshNode(r)
	}
	m.refreshRows()
}

func refreshNode(n *node) {
	if n.kind == nodeFile || !n.loaded {
		return
	}
	old := map[string]*node{}
	for _, c := range n.children {
		old[c.path] = c
	}
	fresh := listDir(n.path, n.member)
	for i, c := range fresh {
		if prev, ok := old[c.path]; ok && prev.kind == c.kind {
			fresh[i] = prev // keep expansion and loaded children
		}
	}
	n.children = fresh
	for _, c := range n.children {
		if c.expanded {
			refreshNode(c)
		}
	}
}

// treeOpView is the dialog over the editor.
func (m *Model) treeOpView(base string) string {
	op := m.treeOp
	var title string
	var lines []string
	switch op.kind {
	case opNew:
		title = "new file or folder"
		lines = append(lines, theme.TextMuted().Render("in "+m.memberRel(op.dir)+"/"))
		lines = append(lines, op.input.View(56)...)
		lines = append(lines, "", theme.Hint().Render("enter create · esc cancel"))
	case opRename:
		title = "rename or move"
		lines = append(lines, theme.TextMuted().Render("path inside "+filepath.Base(m.memberRootOf(op.node.path))+"/"))
		lines = append(lines, op.input.View(56)...)
		lines = append(lines, "", theme.Hint().Render("enter move · esc cancel"))
	case opAgentEdit:
		who, _ := m.helper()
		title = "change with @" + who
		lines = append(lines, theme.TextMuted().Render(fmt.Sprintf("lines %d-%d — the answer comes back as a diff you review", op.region.from+1, op.region.to+1)))
		lines = append(lines, op.input.View(56)...)
		lines = append(lines, "", theme.Hint().Render("enter ask · esc cancel"))
	case opCommand:
		title = "command"
		lines = append(lines, op.input.View(56)...)
		lines = append(lines, "", theme.Hint().Render("enter run · esc cancel"))
	case opDelete:
		title = "delete"
		what := op.node.name
		if op.node.kind != nodeFile {
			what += "/ and everything in it"
		}
		lines = append(lines, theme.TextStyle().Render("Move "+what+" to DHI's trash?"),
			theme.TextMuted().Render("it goes to .dhi/tmp/trash — move it back to restore"),
			"", theme.Hint().Render("y delete · n keep"))
	}
	md := kit.Modal{Title: title, Lines: lines, Error: op.err, Width: 64}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}

// pendingOpenKey handles y/n/enter/esc for the large-file confirmation (F-138).
func (m *Model) pendingOpenKey(key string) bool {
	if m.pendingOpen == nil {
		return false
	}
	switch key {
	case "y", "enter":
		// Confirm and open the file.
		n := m.pendingOpen.node
		m.pendingOpen = nil
		m.openNode(n, true)
		return true
	case "n", "esc":
		// Cancel without opening.
		m.pendingOpen = nil
		m.bufFocus = false
		return true
	}
	return false
}

// largeTextFile reports the size of a regular file at or above the warning
// threshold that looks like text. Only the first binarySniff bytes are read;
// a binary file keeps textbuf's own rejection.
func largeTextFile(path string) (int64, bool) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() < largeFileThreshold {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	head := make([]byte, binarySniff)
	n, _ := io.ReadFull(f, head)
	return fi.Size(), bytes.IndexByte(head[:n], 0) < 0
}

// pendingOpenView renders the large-file confirmation dialog (F-138).
func (m *Model) pendingOpenView(base string) string {
	if m.pendingOpen == nil {
		return base
	}
	lines := []string{
		fmt.Sprintf("Open %s (%s)?", m.agentPath(m.pendingOpen.node.path), kit.FormatBytes(m.pendingOpen.size)),
		theme.TextMuted().Render("files over 5 MiB may take a moment to load"),
		"",
		theme.Hint().Render("y open · n cancel"),
	}
	md := kit.Modal{Title: "large file", Lines: lines, Width: 64}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}
