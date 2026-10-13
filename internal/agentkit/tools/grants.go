package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Grant is one remembered "allow always" (F-067): agent + capability
// scope, and when the human granted it.
type Grant struct {
	Agent string    `json:"agent"`
	Scope string    `json:"scope"`
	At    time.Time `json:"at"`
	Layer Layer     `json:"-"` // which file it lives in (F-087)
}

// Persist loads remembered workspace grants from path (missing = none)
// and saves every later grant or revoke there, so "allow always"
// survives a restart. A malformed file refuses by name rather than
// silently granting nothing or everything (ADR-0011).
func (a *Approvals) Persist(path string) error {
	return a.load(path, LayerWorkspace)
}

// PersistUser does the same for the grants given "everywhere" (F-087):
// <config>/dhi/grants.json, shared by every workspace.
func (a *Approvals) PersistUser(path string) error {
	return a.load(path, LayerEverywhere)
}

func (a *Approvals) load(path string, layer Layer) error {
	gs, err := readGrants(path)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if layer == LayerEverywhere {
		a.userGrantsPath = path
	} else {
		a.grantsPath = path
	}
	if a.grants == nil {
		a.grants = map[string]grantRec{}
	}
	for _, g := range gs {
		if g.Agent == "" || g.Scope == "" {
			continue
		}
		k := grantKey(g.Agent, g.Scope)
		if cur, ok := a.grants[k]; ok && cur.layer == LayerWorkspace {
			continue // the narrower layer wins a duplicate
		}
		a.grants[k] = grantRec{at: g.At, layer: layer}
	}
	return nil
}

func readGrants(path string) ([]Grant, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("approvals: grants: %w", err)
	}
	var gs []Grant
	if len(b) > 0 {
		if err := json.Unmarshal(b, &gs); err != nil {
			return nil, fmt.Errorf("approvals: grants %s: %w", path, err)
		}
	}
	return gs, nil
}

// Targets lists the paths agent may write beyond its sandbox roots
// (F-087): every remembered "write:<path>" grant, any layer.
func (a *Approvals) Targets(agent string) []string {
	var out []string
	for _, g := range a.Grants() {
		if p, ok := strings.CutPrefix(g.Scope, AccessWrite); ok && g.Agent == agent {
			out = append(out, p)
		}
	}
	return out
}

// Grants lists the remembered grants, by agent then scope.
func (a *Approvals) Grants() []Grant {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Grant, 0, len(a.grants))
	for k, rec := range a.grants {
		agent, scope, _ := strings.Cut(k, "|")
		out = append(out, Grant{Agent: agent, Scope: scope, At: rec.at, Layer: rec.layer})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Agent != out[j].Agent {
			return out[i].Agent < out[j].Agent
		}
		return out[i].Scope < out[j].Scope
	})
	return out
}

// Revoke forgets one grant; the next use of that scope asks again.
func (a *Approvals) Revoke(agent, scope string) error {
	a.mu.Lock()
	delete(a.grants, grantKey(agent, scope))
	a.mu.Unlock()
	return a.saveGrants()
}

// saveGrants writes each persisted layer's grants to its file
// atomically ("once" grants live only in memory).
func (a *Approvals) saveGrants() error {
	a.mu.Lock()
	paths := map[Layer]string{LayerWorkspace: a.grantsPath, LayerEverywhere: a.userGrantsPath}
	a.mu.Unlock()
	all := a.Grants()
	for layer, path := range paths {
		if path == "" {
			continue
		}
		gs := []Grant{}
		for _, g := range all {
			if g.Layer == layer {
				gs = append(gs, g)
			}
		}
		if err := writeGrants(path, gs); err != nil {
			return err
		}
	}
	return nil
}

func writeGrants(path string, gs []Grant) error {
	b, err := json.MarshalIndent(gs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("approvals: grants: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("approvals: grants: %w", err)
	}
	return os.Rename(tmp, path)
}
