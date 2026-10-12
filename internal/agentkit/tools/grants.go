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
}

// Persist loads remembered grants from path (missing = none) and saves
// every later grant or revoke there, so "allow always" survives a
// restart. A malformed file refuses by name rather than silently
// granting nothing or everything (ADR-0011).
func (a *Approvals) Persist(path string) error {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("approvals: grants: %w", err)
	}
	var gs []Grant
	if len(b) > 0 {
		if err := json.Unmarshal(b, &gs); err != nil {
			return fmt.Errorf("approvals: grants %s: %w", path, err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.grantsPath = path
	if a.grants == nil {
		a.grants = map[string]time.Time{}
	}
	for _, g := range gs {
		if g.Agent != "" && g.Scope != "" {
			a.grants[grantKey(g.Agent, g.Scope)] = g.At
		}
	}
	return nil
}

// Grants lists the remembered grants, by agent then scope.
func (a *Approvals) Grants() []Grant {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Grant, 0, len(a.grants))
	for k, at := range a.grants {
		agent, scope, _ := strings.Cut(k, "|")
		out = append(out, Grant{Agent: agent, Scope: scope, At: at})
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

// saveGrants writes the grant set atomically when persistence is on.
func (a *Approvals) saveGrants() error {
	a.mu.Lock()
	path := a.grantsPath
	a.mu.Unlock()
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(a.Grants(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("approvals: grants: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("approvals: grants: %w", err)
	}
	return os.Rename(tmp, path)
}
