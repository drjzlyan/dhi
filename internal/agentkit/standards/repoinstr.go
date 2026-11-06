package standards

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// Caps keep a repo's instructions from crowding out the rest of the
// prompt; the budget fit trims further when the whole prompt is tight.
const (
	repoFileRunes  = 6000
	repoTotalRunes = 16000
)

// RepoInstructions renders each member's AGENTS.md for the prompt (F-124).
// A CLI loads AGENTS.md from its own working directory, so that member's
// file is left out when cliReads is true; every other member's file is
// carried, because no CLI looks into sibling repos. roots overrides a
// member's directory (a task worktree holds the branch's copy). The file
// is instructions by design: it is labelled by path, not fenced as
// untrusted data. Whether to run in an unknown repo at all is G-S1's job.
func RepoInstructions(members []workspace.Member, roots map[string]string, cwd string, cliReads bool) string {
	ms := append([]workspace.Member(nil), members...)
	sort.Slice(ms, func(i, j int) bool { return ms[i].Name < ms[j].Name })
	var b strings.Builder
	used := 0
	for _, m := range ms {
		dir := m.Path
		if r, ok := roots[m.Name]; ok && r != "" {
			dir = r
		}
		if cliReads && sameDir(dir, cwd) {
			continue
		}
		path := filepath.Join(dir, "AGENTS.md")
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(raw))
		if text == "" {
			continue
		}
		limit := min(repoFileRunes, repoTotalRunes-used)
		if limit <= 0 {
			b.WriteString("\n(more AGENTS.md files omitted: size cap reached)")
			break
		}
		if rs := []rune(text); len(rs) > limit {
			text = string(rs[:limit]) + "\n… (truncated; read " + path + " for the rest)"
		}
		used += len([]rune(text))
		b.WriteString("\n" + m.Name + "/AGENTS.md:\n" + text + "\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "Repository instructions (the AGENTS.md of repos your CLI does not load itself; follow them when working in that repo):" + b.String()
}

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	if r, err := filepath.EvalSymlinks(b); err == nil {
		b = r
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
