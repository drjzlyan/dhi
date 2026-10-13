package forge

import (
	"regexp"
	"sort"
	"strings"
)

// Suggestion is a command sequence an agent keeps repeating across runs
// (F-069): a candidate for a forged tool.
type Suggestion struct {
	Commands []string // normalised commands, in order
	Runs     int      // distinct runs that contained the sequence
}

var (
	cmdPrefixRe = regexp.MustCompile(`^(bash|command|shell|run):\s*`)
	spaceRe     = regexp.MustCompile(`\s+`)
)

// normCommand reduces a transcript command line to its comparable form:
// prefix dropped, whitespace collapsed, cd-into-a-path prefixes removed.
func normCommand(s string) string {
	s = strings.TrimSpace(cmdPrefixRe.ReplaceAllString(strings.TrimSpace(s), ""))
	if i := strings.Index(s, "&&"); i >= 0 && strings.HasPrefix(s, "cd ") {
		s = strings.TrimSpace(s[i+2:])
	}
	return spaceRe.ReplaceAllString(s, " ")
}

// Detect finds command sequences (length 1..3, the longest first) that
// recur in at least minRuns distinct runs. Each run is the list of
// command-event details from one transcript. Trivial one-word commands
// (ls, pwd, cat …) are ignored: they are not worth a tool.
func Detect(runs [][]string, minRuns int) []Suggestion {
	trivial := map[string]bool{"ls": true, "pwd": true, "cat": true, "git status": true, "ls -la": true}
	count := map[string]int{}
	seqs := map[string][]string{}
	for _, run := range runs {
		var cmds []string
		for _, c := range run {
			if n := normCommand(c); n != "" && !trivial[n] {
				cmds = append(cmds, n)
			}
		}
		seen := map[string]bool{}
		for n := 3; n >= 1; n-- {
			for i := 0; i+n <= len(cmds); i++ {
				seq := cmds[i : i+n]
				key := strings.Join(seq, "\n")
				if !seen[key] {
					seen[key] = true
					count[key]++
					seqs[key] = seq
				}
			}
		}
	}
	var out []Suggestion
	covered := map[string]bool{}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	// Longest sequences first so a recurring triple is not also reported
	// as its three single commands.
	sort.Slice(keys, func(i, j int) bool {
		if li, lj := len(seqs[keys[i]]), len(seqs[keys[j]]); li != lj {
			return li > lj
		}
		if count[keys[i]] != count[keys[j]] {
			return count[keys[i]] > count[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		if count[k] < minRuns || covered[k] {
			continue
		}
		out = append(out, Suggestion{Commands: seqs[k], Runs: count[k]})
		seq := seqs[k]
		for n := 1; n < len(seq); n++ {
			for i := 0; i+n <= len(seq); i++ {
				covered[strings.Join(seq[i:i+n], "\n")] = true
			}
		}
	}
	return out
}
