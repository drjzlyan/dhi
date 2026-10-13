// Package repair finds problems DHI can fix by itself and fixes them in
// the open (F-085, ADR-0038). A repair is named and explained before or
// as it runs: safe ones (DHI's own runtime files) apply at launch with a
// notice; ones that touch what the team tracks wait for the user's yes.
// Nothing here substitutes a default for a broken value: that stays a
// refusal (ADR-0011).
package repair

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Repair is one fix DHI can make.
type Repair struct {
	ID    string // stable, for tests and logs
	What  string // one plain sentence: what is wrong and what DHI will do
	Safe  bool   // touches only DHI's runtime files: applied at launch
	Apply func() error
}

// Input is what Find looks at. Ignored reports whether git already
// ignores a path relative to the workspace root; nil means git cannot be
// asked, and the gitignore repair only writes a plainly missing file.
type Input struct {
	WS      *workspace.Workspace
	Org     *org.Org
	Library *library.Store
	Ignored func(rel string) bool
}

// Find lists the repairs that apply now, safe ones first.
func Find(in Input) []Repair {
	if in.WS == nil {
		return nil
	}
	var out []Repair
	if r, ok := gitignore(in); ok {
		out = append(out, r)
	}
	if in.Org != nil && in.Library != nil {
		out = append(out, libraryRefs(in)...)
	}
	if in.Org != nil {
		out = append(out, danglingTeams(in)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Safe && !out[j].Safe })
	return out
}

// Result is what Run did.
type Result struct {
	Done   []string // What of each repair applied
	Failed []string // What + why for each that failed
}

// Run applies rs (all of them, or only the safe ones).
func Run(rs []Repair, safeOnly bool) Result {
	var res Result
	for _, r := range rs {
		if safeOnly && !r.Safe {
			continue
		}
		if err := r.Apply(); err != nil {
			res.Failed = append(res.Failed, r.What+": "+err.Error())
			continue
		}
		res.Done = append(res.Done, r.What)
	}
	return res
}

// gitignore keeps DHI's runtime state out of git: it writes
// .dhi/.gitignore when missing, or appends entries newer DHI versions
// added. Entries git already ignores (a root .gitignore) are left alone.
func gitignore(in Input) (Repair, bool) {
	path := filepath.Join(in.WS.Root, workspace.DHIDir, ".gitignore")
	have := map[string]bool{}
	exists := false
	if f, err := os.Open(path); err == nil {
		exists = true
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			have[strings.TrimSpace(sc.Text())] = true
		}
		_ = f.Close()
	}
	if in.Ignored == nil {
		// Without git to ask, only the plainly missing file is written,
		// and not when the root .gitignore already speaks for .dhi/.
		root, _ := os.ReadFile(filepath.Join(in.WS.Root, ".gitignore"))
		if exists || strings.Contains(string(root), ".dhi/") {
			return Repair{}, false
		}
	}
	var missing []string
	for _, e := range setup.RuntimeEntries {
		if have[e] {
			continue
		}
		probe := filepath.ToSlash(filepath.Join(workspace.DHIDir, strings.ReplaceAll(strings.TrimSuffix(e, "/"), "*", "x")))
		if strings.HasSuffix(e, "/") {
			probe += "/x"
		}
		if in.Ignored != nil && in.Ignored(probe) {
			continue
		}
		missing = append(missing, e)
	}
	if len(missing) == 0 {
		return Repair{}, false
	}
	names := fmt.Sprintf("%d entries", len(missing))
	if len(missing) <= 3 {
		names = strings.Join(missing, ", ")
	}
	what := "keep DHI's runtime files out of git: add " + names + " to .dhi/.gitignore"
	if !exists {
		what = "keep DHI's runtime files (" + names + ") out of git: write .dhi/.gitignore"
	}
	return Repair{ID: "gitignore", What: what, Safe: true, Apply: func() error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if !exists {
			return os.WriteFile(path, []byte(setup.GitignoreHeader+strings.Join(missing, "\n")+"\n"), 0o644)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, werr := f.WriteString("\n# added by DHI\n" + strings.Join(missing, "\n") + "\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return werr
	}}, true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// libraryRefs finds employees pointing at a role, persona or skill the
// library no longer has; the fix drops the missing reference (their own
// instructions and tools stay).
func libraryRefs(in Input) []Repair {
	roster, err := org.LoadRoster(in.WS)
	if err != nil {
		return nil
	}
	var out []Repair
	for _, a := range roster {
		var gone []string
		if a.Role != "" {
			if _, ok := in.Library.Role(a.Role); !ok {
				gone = append(gone, "role "+a.Role)
			}
		}
		if a.Persona != "" {
			if _, ok := in.Library.Persona(a.Persona); !ok {
				gone = append(gone, "persona "+a.Persona)
			}
		}
		for _, s := range a.Skills {
			if _, ok := in.Library.Skill(s); !ok {
				gone = append(gone, "skill "+s)
			}
		}
		if len(gone) == 0 {
			continue
		}
		agent := a
		out = append(out, Repair{
			ID:   "library-refs:" + a.ID,
			What: fmt.Sprintf("%s uses %s, which no longer exist%s: drop %s (its own instructions and tools stay)", a.ID, strings.Join(gone, ", "), plural(len(gone), "s", ""), plural(len(gone), "it", "them")),
			Apply: func() error {
				fixed := *agent
				if fixed.Role != "" {
					if _, ok := in.Library.Role(fixed.Role); !ok {
						fixed.Role = ""
					}
				}
				if fixed.Persona != "" {
					if _, ok := in.Library.Persona(fixed.Persona); !ok {
						fixed.Persona = ""
					}
				}
				var keep []string
				for _, s := range fixed.Skills {
					if _, ok := in.Library.Skill(s); ok {
						keep = append(keep, s)
					}
				}
				fixed.Skills = keep
				return in.Org.UpdateAgent(in.WS, &fixed)
			},
		})
	}
	return out
}

// danglingTeams finds teams whose lead or members left the roster; the
// fix hands the lead to you and drops the missing members.
func danglingTeams(in Input) []Repair {
	roster, err := org.LoadRoster(in.WS)
	if err != nil {
		return nil
	}
	on := map[string]bool{}
	for _, a := range roster {
		on[a.ID] = true
	}
	for _, id := range in.Org.Archived(in.WS) {
		on[id] = true // archived can be restored: not gone
	}
	var out []Repair
	for _, t := range in.Org.Teams() {
		leadGone := t.Lead != "" && t.Lead != org.Human && !on[t.Lead]
		var keep, gone []string
		for _, mb := range t.Members {
			switch {
			case on[mb]:
				keep = append(keep, mb)
			case mb != t.Lead: // a gone lead is named once, below
				gone = append(gone, mb)
			}
		}
		if !leadGone && len(gone) == 0 {
			continue
		}
		team := t
		lead := t.Lead
		var parts []string
		if leadGone {
			parts = append(parts, "its lead "+t.Lead+" is gone: you lead it")
			lead = org.Human
		}
		if len(gone) > 0 {
			parts = append(parts, "drop missing members "+strings.Join(gone, ", "))
		}
		out = append(out, Repair{
			ID:    "team:" + t.Name,
			What:  "team " + t.Name + ": " + strings.Join(parts, "; "),
			Apply: func() error { return in.Org.UpdateTeam(team.Name, lead, keep) },
		})
	}
	return out
}
