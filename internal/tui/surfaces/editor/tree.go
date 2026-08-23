package editor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type nodeKind uint8

const (
	nodeRepo nodeKind = iota
	nodeDir
	nodeFile
)

// node is one row of the workspace tree. Repo and dir nodes expand;
// file nodes open. Directory children load lazily on first expansion.
type node struct {
	kind     nodeKind
	name     string // display base name
	path     string // absolute filesystem path
	member   string // owning member alias ("" never — set on all)
	children []*node
	expanded bool
	loaded   bool
}

// skipNames are pruned from every listing; deeper ignore rules arrive
// with the git view (gitignore via go-git).
var skipNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	".DS_Store":    true,
}

// buildRoots constructs the repo-level nodes for each workspace member.
func buildRoots(members []memberRef) []*node {
	roots := make([]*node, 0, len(members))
	for _, m := range members {
		roots = append(roots, &node{
			kind:   nodeRepo,
			name:   m.name,
			path:   m.path,
			member: m.name,
		})
	}
	return roots
}

type memberRef struct {
	name string
	path string
}

// toggle expands or collapses repo/dir nodes; files are no-ops.
func (n *node) toggle() {
	if n.kind == nodeFile {
		return
	}
	n.expanded = !n.expanded
	if n.expanded && !n.loaded {
		n.children = listDir(n.path, n.member)
		n.loaded = true
	}
}

// listDir reads one directory into sorted child nodes (dirs first,
// case-insensitive). Read errors yield no children — the filesystem may
// race; doctor surfaces broken trees.
func listDir(dir, member string) []*node {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs, files []*node
	for _, e := range entries {
		name := e.Name()
		if skipNames[name] || strings.HasPrefix(name, ".") && e.IsDir() {
			continue
		}
		child := &node{
			kind:   nodeFile,
			name:   name,
			path:   filepath.Join(dir, name),
			member: member,
		}
		if e.IsDir() {
			child.kind = nodeDir
			dirs = append(dirs, child)
		} else {
			files = append(files, child)
		}
	}
	sortNodes(dirs)
	sortNodes(files)
	return append(dirs, files...)
}

func sortNodes(ns []*node) {
	sort.Slice(ns, func(i, j int) bool {
		return strings.ToLower(ns[i].name) < strings.ToLower(ns[j].name)
	})
}

// treeRow is one visible row with its nesting depth.
type treeRow struct {
	node  *node
	depth int
}

// flatten renders expanded structure as visible rows with depths.
func flatten(roots []*node) []treeRow {
	var out []treeRow
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		out = append(out, treeRow{node: n, depth: depth})
		if n.expanded {
			for _, c := range n.children {
				walk(c, depth+1)
			}
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

// indexFiles walks every member collecting file paths relative to their
// repo root ("member/rel/path") for fuzzy find. Hard cap keeps huge
// checkouts responsive; deep indexing returns what fit.
func indexFiles(roots []*node, cap int) []string {
	var out []string
	var walk func(n *node, prefix string)
	walk = func(n *node, prefix string) {
		if len(out) >= cap {
			return
		}
		for _, c := range n.children {
			switch c.kind {
			case nodeRepo:
				walk(c, c.name+"/")
			case nodeDir:
				if !c.loaded {
					c.children = listDir(c.path, c.member)
					c.loaded = true
				}
				walk(c, prefix+c.name+"/")
			case nodeFile:
				out = append(out, prefix+c.name)
			}
		}
	}
	for _, r := range roots {
		if !r.loaded {
			r.children = listDir(r.path, r.member)
			r.loaded = true
		}
		walk(r, r.name+"/")
	}
	return out
}

// findByPath locates a node by absolute path within loaded structure.
func findByPath(roots []*node, path string) *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		if found != nil {
			return
		}
		if n.path == path {
			found = n
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return found
}

// revealTo expands ancestors so the node at path becomes visible.
func revealTo(roots []*node, path string) bool {
	target := filepath.Dir(path)
	for _, r := range roots {
		if expandTowards(r, target, path) {
			return true
		}
	}
	return false
}

// expandTowards expands the chain from n towards target (the directory
// that should end up open); returns true when reached.
func expandTowards(n *node, target, leaf string) bool {
	if n.kind != nodeRepo && n.kind != nodeDir {
		return false
	}
	within := n.path == target ||
		strings.HasPrefix(target, n.path+string(os.PathSeparator)) ||
		strings.HasPrefix(leaf, n.path+string(os.PathSeparator))
	if !within {
		return false
	}
	if !n.expanded {
		n.toggle()
	}
	for _, c := range n.children {
		if c.path == target {
			if c.kind != nodeFile && !c.expanded {
				c.toggle()
			}
			return true
		}
		if expandTowards(c, target, leaf) {
			return true
		}
	}
	return n.path == target
}
