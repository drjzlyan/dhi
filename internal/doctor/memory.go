package doctor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// bigLog is the size past which a .dhi log is called out: prompts read
// it by tail, but it still costs disk and the UI's full-history views.
const bigLog = 32 << 20

// Memory reports the health of DHI's runtime stores (F-078): lines a
// store quarantined as corrupt (kept in *.corrupt so nothing is lost),
// interrupted repairs, and logs that have grown very large.
func Memory(wsRoot string) []Check {
	if wsRoot == "" {
		return nil
	}
	dhi := filepath.Join(wsRoot, workspace.DHIDir)
	if _, err := os.Stat(dhi); err != nil {
		return nil
	}
	var corrupt, repairs, big []string
	_ = filepath.WalkDir(dhi, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "tasks", "reviews", "sessions": // worktrees: not DHI's stores
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(wsRoot, p)
		switch {
		case strings.HasSuffix(p, ".corrupt"):
			corrupt = append(corrupt, rel)
		case strings.HasSuffix(p, ".repair"):
			repairs = append(repairs, rel)
		case strings.HasSuffix(p, ".jsonl"):
			if info, err := d.Info(); err == nil && info.Size() > bigLog {
				big = append(big, fmt.Sprintf("%s (%d MB)", rel, info.Size()>>20))
			}
		}
		return nil
	})
	sort.Strings(corrupt)
	sort.Strings(repairs)
	sort.Strings(big)
	var out []Check
	if len(repairs) > 0 {
		out = append(out, Check{Name: "memory/repair", Status: Fail,
			Detail: "a store repair was interrupted; its recovery copy holds the original: " + strings.Join(repairs, ", ")})
	}
	if len(corrupt) > 0 {
		out = append(out, Check{Name: "memory/quarantine", Status: Warn,
			Detail: "corrupt lines were moved aside (the rest stayed readable): " + strings.Join(corrupt, ", ")})
	}
	if len(big) > 0 {
		out = append(out, Check{Name: "memory/size", Status: Warn,
			Detail: "large logs (prompts read them by tail; consider archiving): " + strings.Join(big, ", ")})
	}
	if len(out) == 0 {
		out = append(out, Check{Name: "memory", Status: OK, Detail: "stores readable, nothing quarantined"})
	}
	return out
}
