package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
)

// forgeHintRuns is how many of an agent's recent transcripts the
// repetition detector reads (local, no tokens).
const forgeHintRuns = 20

// forgeHint nudges an agent toward tool_propose when it keeps running the
// same commands across runs (F-069). It says nothing when a forged tool
// (active or pending) already covers the sequence.
func (r *Runtime) forgeHint(agentID string) string {
	if r.cfg.Forge == nil {
		return ""
	}
	runs := r.recentCommands(agentID, forgeHintRuns)
	if len(runs) < 3 {
		return ""
	}
	covered := map[string]bool{}
	for _, list := range [][]forge.Tool{mustTools(r.cfg.Forge.Active()), mustTools(r.cfg.Forge.Pending())} {
		for _, t := range list {
			for _, st := range t.Steps {
				covered[strings.Join(st.Run, " ")] = true
			}
		}
	}
	for _, s := range forge.Detect(runs, 3) {
		if len(s.Commands) == 1 && covered[s.Commands[0]] || allCovered(s.Commands, covered) {
			continue
		}
		cmds := make([]string, len(s.Commands))
		for i, c := range s.Commands {
			cmds[i] = "`" + truncateRunes(c, 80) + "`"
		}
		return fmt.Sprintf("\n\nYou keep repeating %s (in %d of your recent runs). If it will keep coming up, "+
			"propose it as a reusable tool with tool_propose — the human approves it once and every agent can use it.",
			strings.Join(cmds, " then "), s.Runs)
	}
	return ""
}

func allCovered(cmds []string, covered map[string]bool) bool {
	for _, c := range cmds {
		if !covered[c] {
			return false
		}
	}
	return true
}

func mustTools(ts []forge.Tool, err error) []forge.Tool {
	if err != nil {
		return nil
	}
	return ts
}

// recentCommands reads the command events of an agent's latest n run
// transcripts (.dhi/agents/<id>/runs/*.jsonl), newest last.
func (r *Runtime) recentCommands(agentID string, n int) [][]string {
	dir := filepath.Join(r.cfg.WS.Root, ".dhi", "agents", agentID, "runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // run ids are timestamps
	if len(names) > n {
		names = names[len(names)-n:]
	}
	var out [][]string
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var cmds []string
		for _, line := range strings.Split(string(b), "\n") {
			var ev struct{ Kind, Detail string }
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Kind == clirun.EventCommand {
				cmds = append(cmds, ev.Detail)
			}
		}
		if len(cmds) > 0 {
			out = append(out, cmds)
		}
	}
	return out
}
