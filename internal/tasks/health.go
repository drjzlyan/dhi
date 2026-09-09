package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// CheckRuns is the F-014 §Part C audit for the doctor `runs/store` row.
// It scans every card file's [[run]] blocks for schema violations —
// naming the card and the offending line — and checks that agent
// transcript dirs referenced by runs are readable. An absent tasks dir
// (or runs dir — nothing recorded yet) reports nothing.
func CheckRuns(wsRoot string) []string {
	dir := filepath.Join(wsRoot, Dir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []string{fmt.Sprintf("tasks: read %s: %v", dir, err)}
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".toml")
		out = append(out, checkCardRuns(filepath.Join(dir, e.Name()), slug, wsRoot)...)
	}
	sort.Strings(out)
	return out
}

// checkCardRuns validates one card's run blocks against raw line
// numbers so warnings can point exactly (F-014: "naming the card + line").
func checkCardRuns(path, slug, wsRoot string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", slug, err)}
	}
	lines := strings.Split(string(data), "\n")

	var out []string
	var transDirs []string
	blockStart := -1
	var block []string
	flush := func() {
		if blockStart < 0 {
			return
		}
		warn, td := checkRunBlock(slug, wsRoot, lines, blockStart, block)
		out = append(out, warn...)
		transDirs = append(transDirs, td...)
		blockStart, block = -1, nil
	}
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "[[run]]" {
			flush()
			blockStart = i + 1 // 1-based line
			continue
		}
		if strings.HasPrefix(trim, "[") && blockStart >= 0 {
			flush()
		}
		if blockStart >= 0 {
			block = append(block, ln)
		}
	}
	flush()

	for _, d := range sortUnique(transDirs) {
		if _, err := os.Stat(d); err != nil {
			out = append(out, fmt.Sprintf("%s: transcript dir %s: unreadable", slug, d))
		}
	}
	return out
}

// checkRunBlock decodes one [[run]] block in isolation and validates it
// with the same strict rules as parseCard, reporting the field's line.
// wsRoot resolves relative transcript paths (cards store .dhi/… paths).
func checkRunBlock(slug, wsRoot string, lines []string, start int, block []string) (warn []string, transDirs []string) {
	snippet := "[[run]]\n" + strings.Join(block, "\n")
	var f struct {
		Runs []Run `toml:"run"`
	}
	md, err := toml.Decode(snippet, &f)
	if err != nil {
		warn = append(warn, fmt.Sprintf("%s @L%d: %v", slug, start, err))
		return warn, nil
	}
	if und := md.Undecoded(); len(und) > 0 {
		warn = append(warn, fmt.Sprintf("%s @L%d: unknown run key(s): %s",
			slug, start, und[0].String()))
		return warn, nil
	}
	if len(f.Runs) != 1 {
		warn = append(warn, fmt.Sprintf("%s @L%d: run block decodes to %d run(s), want 1",
			slug, start, len(f.Runs)))
		return warn, nil
	}
	r := f.Runs[0]
	if verr := validateRun(r); verr != nil {
		ln := offendingLine(lines, start, verr.Error())
		warn = append(warn, fmt.Sprintf("%s run @L%d: %v", slug, ln, verr))
		return warn, nil
	}
	if r.Transcript != "" {
		td := filepath.Dir(filepath.Clean(r.Transcript))
		if !filepath.IsAbs(td) {
			td = filepath.Join(wsRoot, td)
		}
		transDirs = append(transDirs, td)
	}
	return warn, transDirs
}

// offendingLine maps a validateRun error to the block's field line.
func offendingLine(lines []string, start int, msg string) int {
	key := ""
	switch {
	case strings.Contains(msg, "status"):
		key = "status"
	case strings.Contains(msg, "tokens"):
		key = "tokens_"
	case strings.Contains(msg, "started"), strings.Contains(msg, "finished"):
		key = "started"
	case strings.Contains(msg, "agent"):
		key = "agent"
	case strings.Contains(msg, "id"):
		key = "id"
	}
	if key == "" {
		return start
	}
	for i, ln := range lines {
		if i+1 < start {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(ln), key) {
			return i + 1
		}
	}
	return start
}

func sortUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
