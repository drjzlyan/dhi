// Package search runs content searches across workspace members. The
// production backend runs the user's ripgrep (F-110); results stream so
// large trees stay responsive.
package search

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

// Ripgrep searches with an rg binary at Bin.
type Ripgrep struct {
	Bin string
}

// maxHits bounds any single search; fan-out across many repos must not
// flood the UI.
const maxHits = 2000

// RegexSearcher is implemented by searchers that also take a regular
// expression (F-056: the editor's regex mode). Searchers without it
// stay fixed-string only.
type RegexSearcher interface {
	SearchRegex(ctx context.Context, pattern string, roots []string) (<-chan Hit, error)
}

// Search implements Searcher (fixed-string).
func (r Ripgrep) Search(ctx context.Context, query string, roots []string) (<-chan Hit, error) {
	return r.run(ctx, query, roots, true)
}

// SearchRegex implements RegexSearcher (rg's regex syntax).
func (r Ripgrep) SearchRegex(ctx context.Context, pattern string, roots []string) (<-chan Hit, error) {
	return r.run(ctx, pattern, roots, false)
}

func (r Ripgrep) run(ctx context.Context, query string, roots []string, fixed bool) (<-chan Hit, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("search: no roots given")
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("search: empty query")
	}
	args := []string{"--json", "-S", "--no-require-git", "--max-filesize", "1M"}
	if fixed {
		args = append(args, "-F")
	}
	cmd := exec.CommandContext(ctx, r.Bin, append(args, "-e", query)...)
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

// MissingToolError says the searcher's program is not installed; the
// editor turns it into the install card (F-110).
type MissingToolError struct{ Tool string }

func (e *MissingToolError) Error() string { return e.Tool + " is not installed" }

// Host searches with the user's own ripgrep, looked up at every search so
// one installed mid-session is used without a restart (F-110).
type Host struct {
	Look func(name string) (string, bool)
}

func (h Host) rg() (Ripgrep, error) {
	if h.Look != nil {
		if p, ok := h.Look("rg"); ok {
			return Ripgrep{Bin: p}, nil
		}
	}
	return Ripgrep{}, &MissingToolError{Tool: "rg"}
}

// Search implements Searcher.
func (h Host) Search(ctx context.Context, query string, roots []string) (<-chan Hit, error) {
	r, err := h.rg()
	if err != nil {
		return nil, err
	}
	return r.Search(ctx, query, roots)
}

// SearchRegex implements RegexSearcher.
func (h Host) SearchRegex(ctx context.Context, pattern string, roots []string) (<-chan Hit, error) {
	r, err := h.rg()
	if err != nil {
		return nil, err
	}
	return r.SearchRegex(ctx, pattern, roots)
}

// Refused is the explicit "capability not installed" searcher
// (ADR-0011): instead of the search key being silently inert, every
// query errors with the named fix, which the results view renders.
type Refused struct{ Reason string }

// Search implements Searcher by refusing.
func (r Refused) Search(ctx context.Context, query string, roots []string) (<-chan Hit, error) {
	return nil, errors.New(r.Reason)
}
