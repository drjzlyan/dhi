// Package starter provides ready-made teams of employees (F-045): a
// template names a team, its lead and its members; each member is a
// role + persona + skills from the behaviour library. Applying one writes
// ordinary agent manifests and an org team — nothing here is special at
// runtime, so every employee is editable like one made by hand.
package starter

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/workspace"
)

//go:embed templates/*.toml
var templatesFS embed.FS

// Employee is one member of a template.
type Employee struct {
	ID      string
	Name    string
	Role    string
	Persona string
	Skills  []string
}

// Template is a team blueprint.
type Template struct {
	Slug        string
	Name        string
	Description string
	Team        string // org team slug
	Lead        string // an employee id, or org.Human
	Employees   []Employee
}

type tplFile struct {
	Schema      int    `toml:"schema"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Team        string `toml:"team"`
	Lead        string `toml:"lead"`
	Employee    []struct {
		ID      string   `toml:"id"`
		Name    string   `toml:"name"`
		Role    string   `toml:"role"`
		Persona string   `toml:"persona"`
		Skills  []string `toml:"skills"`
	} `toml:"employee"`
}

func parse(slug string, data []byte) (Template, error) {
	var f tplFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Template{}, err
	}
	if und := md.Undecoded(); len(und) > 0 {
		var keys []string
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return Template{}, fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
	}
	if f.Schema != 1 {
		return Template{}, fmt.Errorf("schema %d, want 1", f.Schema)
	}
	t := Template{Slug: slug, Name: f.Name, Description: f.Description, Team: f.Team, Lead: f.Lead}
	seen := map[string]bool{}
	for _, e := range f.Employee {
		if seen[e.ID] {
			return Template{}, fmt.Errorf("duplicate employee %q", e.ID)
		}
		seen[e.ID] = true
		t.Employees = append(t.Employees, Employee{ID: e.ID, Name: e.Name, Role: e.Role, Persona: e.Persona, Skills: e.Skills})
	}
	if t.Name == "" || t.Description == "" || t.Team == "" {
		return Template{}, fmt.Errorf("name, description and team are required")
	}
	if len(t.Employees) == 0 {
		return Template{}, fmt.Errorf("a template needs at least one employee")
	}
	if t.Lead != org.Human && !seen[t.Lead] {
		return Template{}, fmt.Errorf("lead %q is neither %q nor an employee", t.Lead, org.Human)
	}
	return t, nil
}

// Templates lists the built-in templates, smallest team first.
func Templates() []Template {
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil
	}
	var out []Template
	for _, e := range entries {
		data, err := templatesFS.ReadFile("templates/" + e.Name())
		if err != nil {
			continue
		}
		t, err := parse(strings.TrimSuffix(e.Name(), ".toml"), data)
		if err != nil {
			continue // covered by TestEveryTemplateParses
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Employees) != len(out[j].Employees) {
			return len(out[i].Employees) < len(out[j].Employees)
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// Get returns one template by slug.
func Get(slug string) (Template, bool) {
	for _, t := range Templates() {
		if t.Slug == slug {
			return t, true
		}
	}
	return Template{}, false
}

// Result reports what Apply did.
type Result struct {
	Created     []string // employee ids written
	Skipped     []string // ids that already existed (left untouched)
	Team        string
	TeamCreated bool // false when the team already existed
}

// build turns an employee into a validated manifest. Tools and the
// sandbox policy come from the role, never from the template.
func build(lib *library.Store, e Employee) (*manifest.Agent, error) {
	role, ok := lib.Role(e.Role)
	if !ok {
		return nil, fmt.Errorf("%s: role %q is not in the library", e.ID, e.Role)
	}
	if e.Persona != "" {
		if _, ok := lib.Persona(e.Persona); !ok {
			return nil, fmt.Errorf("%s: persona %q is not in the library", e.ID, e.Persona)
		}
	}
	for _, s := range e.Skills {
		if _, ok := lib.Skill(s); !ok {
			return nil, fmt.Errorf("%s: skill %q is not in the library", e.ID, s)
		}
	}
	a := &manifest.Agent{
		ID: e.ID, Name: e.Name, Model: manifest.ModelDefault,
		Role: e.Role, Persona: e.Persona, Skills: append([]string(nil), e.Skills...),
		Tools: append([]string(nil), role.Tools...),
	}
	policy, err := library.PolicyPresetJSON(role.Preset)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.ID, err)
	}
	if err := a.SetPolicyJSON(policy); err != nil {
		return nil, err
	}
	if _, err := manifest.Marshal(a); err != nil { // strict round trip
		return nil, err
	}
	return a, nil
}

// Apply writes the template's employees and team. It validates every
// manifest before the first write, never overwrites an existing agent
// (those are reported as Skipped), refuses when an id is archived, and
// removes what it wrote if a later step fails — all or nothing.
func Apply(ws *workspace.Workspace, o *org.Org, lib *library.Store, t Template) (Result, error) {
	res := Result{Team: t.Team}
	agents := make([]*manifest.Agent, 0, len(t.Employees))
	for _, e := range t.Employees {
		a, err := build(lib, e)
		if err != nil {
			return res, fmt.Errorf("starter %s: %w", t.Slug, err)
		}
		agents = append(agents, a)
	}
	existing := map[string]bool{}
	roster, err := org.LoadRoster(ws)
	if err != nil {
		return res, fmt.Errorf("starter %s: %w", t.Slug, err)
	}
	for _, a := range roster {
		existing[a.ID] = true
	}
	archived := map[string]bool{}
	for _, id := range o.Archived(ws) {
		archived[id] = true
	}
	for _, a := range agents {
		if archived[a.ID] {
			return res, fmt.Errorf("starter %s: agent %q is archived — restore or delete it first", t.Slug, a.ID)
		}
	}

	var created []string
	rollback := func() {
		for _, id := range created {
			_ = o.DeleteAgent(ws, id)
		}
	}
	for _, a := range agents {
		if existing[a.ID] {
			res.Skipped = append(res.Skipped, a.ID)
			continue
		}
		if err := o.CreateAgent(ws, a); err != nil {
			rollback()
			return Result{Team: t.Team}, fmt.Errorf("starter %s: %w", t.Slug, err)
		}
		created = append(created, a.ID)
	}
	res.Created = created

	if _, exists := o.Team(t.Team); !exists {
		ids := make([]string, len(agents))
		for i, a := range agents {
			ids[i] = a.ID
		}
		if err := o.CreateTeam(t.Team, t.Lead, ids); err != nil {
			rollback()
			return Result{Team: t.Team}, fmt.Errorf("starter %s: %w", t.Slug, err)
		}
		res.TeamCreated = true
	}
	return res, nil
}
