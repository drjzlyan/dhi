package workspace

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// VPath names one file across the whole workspace:
// "<member>/<rel-path>" (e.g. "dhi/internal/theme/theme.go"), always
// slash-separated regardless of host OS. The member name never starts
// with "." so reserved `.dhi/...` paths can never collide with members.
type VPath struct {
	Member string
	Rel    string // slash-separated, no leading/trailing slashes; "" = member root
}

// ParseVPath splits a textual vpath.
func ParseVPath(s string) (VPath, error) {
	s = strings.TrimPrefix(s, "@")
	if s == "" {
		return VPath{}, fmt.Errorf("workspace: empty vpath")
	}
	member, rel, _ := strings.Cut(s, "/")
	if !validName(member) {
		return VPath{}, fmt.Errorf("workspace: bad vpath member %q", member)
	}
	rel = strings.Trim(rel, "/")
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || seg == "." {
			return VPath{}, fmt.Errorf("workspace: vpath %q escapes its member", s)
		}
	}
	return VPath{Member: member, Rel: rel}, nil
}

// String renders the canonical textual form.
func (v VPath) String() string {
	if v.Rel == "" {
		return v.Member
	}
	return v.Member + "/" + v.Rel
}

// Resolve maps a vpath to its absolute filesystem location.
func (w *Workspace) Resolve(v VPath) (string, error) {
	m, ok := w.Member(v.Member)
	if !ok {
		return "", fmt.Errorf("workspace: unknown member %q", v.Member)
	}
	clean := path.Clean(v.Rel)
	if clean == "." {
		return m.Path, nil
	}
	if strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("workspace: vpath %q escapes its member", v.String())
	}
	return filepath.Join(m.Path, filepath.FromSlash(clean)), nil
}

// VPathFor is the reverse mapping: absolute path → vpath. Paths outside
// every member are rejected.
func (w *Workspace) VPathFor(abs string) (VPath, error) {
	abs = filepath.Clean(abs)
	for _, m := range w.Members {
		if abs == m.Path {
			return VPath{Member: m.Name}, nil
		}
		rel, err := filepath.Rel(m.Path, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		return VPath{Member: m.Name, Rel: filepath.ToSlash(rel)}, nil
	}
	return VPath{}, fmt.Errorf("workspace: %s is outside all members", abs)
}
