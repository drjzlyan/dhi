package editor

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// Merge conflicts (F-156): the buffer jumps and accepts sides itself
// (textbuf); this file adds the git side. The panel marks unmerged files
// (UU, AA, ...), and once the last conflict marker of such a file is gone
// and the buffer is saved, a y/n prompt offers `git add` on it.

// relInRepo is path relative to the git root, "" when outside it.
func (m *Model) relInRepo(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// offerConflictStage runs after a key: a buffer that had conflict markers
// and now has none and is saved gets the stage prompt, if git still lists
// the file as unmerged.
func (m *Model) offerConflictStage(e *textbuf.Editor) {
	if len(e.Conflicts()) > 0 {
		if m.conflictSeen == nil {
			m.conflictSeen = map[*textbuf.Editor]bool{}
		}
		m.conflictSeen[e] = true
		return
	}
	if !m.conflictSeen[e] || e.Buffer().Dirty() || m.gitCLI == nil {
		return
	}
	delete(m.conflictSeen, e)
	root := m.currentGitRoot()
	if root == "" {
		return
	}
	rel := m.relInRepo(root, e.Path())
	if rel == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := gitcore.Conflicts(ctx, m.gitCLI, root)
	if err != nil {
		e.SetMessage(err.Error())
		return
	}
	for _, c := range list {
		if c.Path == rel {
			m.conflictStage = e.Path()
			e.SetMessage("all conflicts resolved: stage " + rel + "? (y/n)")
			return
		}
	}
}

// answerConflictStage consumes the answer to the stage prompt: y runs
// git add; any other key dismisses it (and, except esc and n, carries on).
func (m *Model) answerConflictStage(e *textbuf.Editor, key string) bool {
	path := m.conflictStage
	m.conflictStage = ""
	if key != "y" {
		e.SetMessage("")
		return key == "n" || key == "esc"
	}
	root := m.currentGitRoot()
	rel := m.relInRepo(root, path)
	if root == "" || rel == "" || m.gitCLI == nil {
		e.SetMessage("cannot stage: not in a git repository")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := gitcore.StagePath(ctx, m.gitCLI, root, rel); err != nil {
		e.SetMessage(err.Error())
		return true
	}
	e.SetMessage("staged " + rel)
	m.gitMessage = "staged " + rel
	m.invalidateGutters()
	m.invalidateTreeGit()
	m.refreshGitData()
	return true
}
