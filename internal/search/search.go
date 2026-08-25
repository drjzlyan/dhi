// Package search runs content searches across workspace members. The
// production backend shells out to DHI's own ripgrep (installed via the
// hermetic toolchain, invoked with the shim path — never the host copy,
// ADR-0005). Results stream so large trees stay responsive.
package search

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Hit is one matched line.
type Hit struct {
	Path   string // absolute file path
	Line   int    // 1-based line number
	Column int    // 0-based byte column of first submatch
	Text   string // full line content (newline stripped)
}

// Searcher executes a query over one or more roots.
type Searcher interface {
	// Search streams hits until the context is cancelled or the search
	// completes; the channel is closed either way. Errors surface via
	// the error return (startup problems) or close the stream early.
	Search(ctx context.Context, query string, roots []string) (<-chan Hit, error)
}

// Ripgrep searches with an rg binary at Bin (DHI's shim path).
type Ripgrep struct {
	Bin string
}

// maxHits bounds any single search; fan-out across many repos must not
// flood the UI.
const maxHits = 2000

// Search implements Searcher.
func (r Ripgrep) Search(ctx context.Context, query string, roots []string) (<-chan Hit, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("search: no roots given")
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("search: empty query")
	}
	cmd := exec.CommandContext(ctx, r.Bin,
		"--json", "-F", "-S", "--no-require-git",
		"--max-filesize", "1M",
		query,
	)
	cmd.Args = append(cmd.Args, roots...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("search: pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("search: start rg: %w", err)
	}

	hits := make(chan Hit, 64)
	go func() {
		defer close(hits)
		defer func() { _ = cmd.Wait() }()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sent := 0
		for sc.Scan() && sent < maxHits && ctx.Err() == nil {
			hit, ok := parseRipgrepJSON(sc.Bytes())
			if !ok {
				continue
			}
			select {
			case hits <- hit:
				sent++
			case <-ctx.Done():
				return
			}
		}
	}()
	return hits, nil
}

// rgMatch mirrors the subset of rg --json output DHI consumes.
type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		LineNumber int `json:"line_number"`
		Lines      struct {
			Text string `json:"text"`
		} `json:"lines"`
		Submatches []struct {
			Match struct {
				Start int `json:"start"`
			} `json:"match"`
		} `json:"submatches"`
	} `json:"data"`
}

// parseRipgrepJSON decodes one NDJSON event; only "match" events yield
// hits, everything else (begin/end/summary) is ignored.
func parseRipgrepJSON(line []byte) (Hit, bool) {
	var ev rgEvent
	if err := json.Unmarshal(line, &ev); err != nil || ev.Type != "match" {
		return Hit{}, false
	}
	col := 0
	if len(ev.Data.Submatches) > 0 {
		col = ev.Data.Submatches[0].Match.Start
	}
	return Hit{
		Path:   ev.Data.Path.Text,
		Line:   ev.Data.LineNumber,
		Column: col,
		Text:   strings.TrimSuffix(ev.Data.Lines.Text, "\n"),
	}, true
}

// Format renders a hit for list display.
func (h Hit) String() string {
	return h.Path + ":" + strconv.Itoa(h.Line) + ": " + h.Text
}
